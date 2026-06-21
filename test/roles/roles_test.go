// Package roles_test is the end-to-end regression for the ownership-transfer
// fixes (review findings #1 and #2). It drives the real memberships.Service
// against a real Postgres as app_user and asserts the single-owner invariant
// holds across every role-change path.
//
// Requires DATABASE_URL (app_user DSN); skips otherwise, like the isolation
// suite:
//
//	DATABASE_URL='postgres://app_user:devpassword@localhost:5433/statusflow?sslmode=disable' \
//	  go test ./test/roles/...
package roles_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/authz"
	"github.com/ishaangarg9/statusflow/internal/db"
	"github.com/ishaangarg9/statusflow/internal/domain/memberships"
	"github.com/ishaangarg9/statusflow/internal/shared"
	"github.com/ishaangarg9/statusflow/internal/tenancy"
)

type fixture struct {
	pool              *pgxpool.Pool
	svc               *memberships.Service
	org               uuid.UUID
	alice, bob, carol uuid.UUID // alice=owner, bob=admin, carol=member
}

func setup(t *testing.T) (context.Context, fixture) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping role/ownership regression (needs real Postgres as app_user)")
	}
	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("connect as app_user: %v", err)
	}
	t.Cleanup(pool.Close)

	suffix := uuid.NewString()[:8]
	f := fixture{
		pool: pool, svc: memberships.NewService(pool),
		org:   uuid.New(),
		alice: uuid.New(), bob: uuid.New(), carol: uuid.New(),
	}

	users := []struct {
		id    uuid.UUID
		email string
	}{
		{f.alice, "roles-" + suffix + "-alice@example.com"},
		{f.bob, "roles-" + suffix + "-bob@example.com"},
		{f.carol, "roles-" + suffix + "-carol@example.com"},
	}
	for _, u := range users {
		if _, err := pool.Exec(ctx,
			`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'x')`,
			u.id, u.email); err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}

	err = tenancy.WithOrgTx(ctx, pool, f.org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO organizations (id, name, slug) VALUES ($1, $2, $3)`,
			f.org, "roles-"+suffix, "roles-"+suffix); err != nil {
			return err
		}
		for _, m := range []struct {
			user uuid.UUID
			role string
		}{{f.alice, "owner"}, {f.bob, "admin"}, {f.carol, "member"}} {
			if _, err := tx.Exec(ctx,
				`INSERT INTO memberships (org_id, user_id, role) VALUES ($1, $2, $3)`,
				f.org, m.user, m.role); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed org: %v", err)
	}

	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = tenancy.WithOrgTx(cctx, pool, f.org, func(tx pgx.Tx) error {
			_, err := tx.Exec(cctx, `DELETE FROM organizations WHERE id = $1`, f.org)
			return err
		})
		_, _ = pool.Exec(cctx, `DELETE FROM users WHERE id = ANY($1)`,
			[]uuid.UUID{f.alice, f.bob, f.carol})
	})

	return ctx, f
}

func (f fixture) ac(user uuid.UUID, role authz.Role) authz.AuthContext {
	return authz.AuthContext{UserID: user, OrgID: f.org, Role: role}
}

func (f fixture) ownerCount(ctx context.Context, t *testing.T) int {
	t.Helper()
	var n int
	if err := tenancy.WithOrgTx(ctx, f.pool, f.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM memberships WHERE org_id = $1 AND role = 'owner'`,
			f.org).Scan(&n)
	}); err != nil {
		t.Fatalf("count owners: %v", err)
	}
	return n
}

func (f fixture) roleOf(ctx context.Context, t *testing.T, user uuid.UUID) string {
	t.Helper()
	var r string
	if err := tenancy.WithOrgTx(ctx, f.pool, f.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT role FROM memberships WHERE org_id = $1 AND user_id = $2`,
			f.org, user).Scan(&r)
	}); err != nil {
		t.Fatalf("role of %s: %v", user, err)
	}
	return r
}

func forbidden(err error) bool {
	var ae *shared.AppError
	return errors.As(err, &ae) && ae.Status == 403
}

// Finding #1: an admin must not be able to promote a member to owner. The org
// must still have exactly one owner (alice) afterward.
func TestAdminCannotMintSecondOwner(t *testing.T) {
	ctx, f := setup(t)

	_, err := f.svc.UpdateRole(ctx, f.ac(f.bob, authz.RoleAdmin), f.carol, authz.RoleOwner)
	if err == nil {
		t.Fatal("admin was allowed to promote a member to owner")
	}
	if !forbidden(err) {
		t.Fatalf("expected 403 forbidden, got %v", err)
	}
	if n := f.ownerCount(ctx, t); n != 1 {
		t.Fatalf("expected exactly 1 owner after blocked promotion, got %d", n)
	}
	if r := f.roleOf(ctx, t, f.carol); r != "member" {
		t.Fatalf("carol should still be 'member', got %q", r)
	}
}

// Finding #2: the sole owner must not be able to demote themselves, which would
// leave the org with zero owners.
func TestOwnerCannotSelfDemoteToZeroOwners(t *testing.T) {
	ctx, f := setup(t)

	_, err := f.svc.UpdateRole(ctx, f.ac(f.alice, authz.RoleOwner), f.alice, authz.RoleAdmin)
	if err == nil {
		t.Fatal("sole owner was allowed to demote themselves")
	}
	if !forbidden(err) {
		t.Fatalf("expected 403 forbidden, got %v", err)
	}
	if n := f.ownerCount(ctx, t); n != 1 {
		t.Fatalf("expected exactly 1 owner after blocked self-demotion, got %d", n)
	}
	if r := f.roleOf(ctx, t, f.alice); r != "owner" {
		t.Fatalf("alice should still be 'owner', got %q", r)
	}
}

// The legitimate path: the owner transfers ownership to another member, which
// atomically demotes the old owner. Exactly one owner remains throughout.
func TestOwnerCanTransferOwnership(t *testing.T) {
	ctx, f := setup(t)

	if _, err := f.svc.UpdateRole(ctx, f.ac(f.alice, authz.RoleOwner), f.bob, authz.RoleOwner); err != nil {
		t.Fatalf("legit ownership transfer failed: %v", err)
	}
	if n := f.ownerCount(ctx, t); n != 1 {
		t.Fatalf("expected exactly 1 owner after transfer, got %d", n)
	}
	if r := f.roleOf(ctx, t, f.bob); r != "owner" {
		t.Fatalf("bob should be 'owner' after transfer, got %q", r)
	}
	if r := f.roleOf(ctx, t, f.alice); r != "admin" {
		t.Fatalf("alice should be demoted to 'admin' after transfer, got %q", r)
	}
}

// An owner cannot be removed directly; ownership must be transferred first.
func TestOwnerCannotBeRemoved(t *testing.T) {
	ctx, f := setup(t)

	err := f.svc.Remove(ctx, f.ac(f.bob, authz.RoleAdmin), f.alice)
	if err == nil {
		t.Fatal("admin was allowed to remove the owner")
	}
	if !forbidden(err) {
		t.Fatalf("expected 403 forbidden, got %v", err)
	}
	if n := f.ownerCount(ctx, t); n != 1 {
		t.Fatalf("expected exactly 1 owner after blocked removal, got %d", n)
	}
}
