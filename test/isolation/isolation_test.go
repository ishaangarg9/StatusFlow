// Package isolation_test is the proof: it connects as the restricted app_user
// role against a REAL Postgres with RLS forced on, and demonstrates that one
// org cannot see or write another org's rows — even when the app-level
// WHERE org_id filter is deliberately omitted.
//
// Requires a running database. Set DATABASE_URL to the app_user DSN, e.g.:
//
//	DATABASE_URL='postgres://app_user:devpassword@localhost:5433/statusflow?sslmode=disable' \
//	  go test ./test/isolation/...
//
// The test skips (not fails) when DATABASE_URL is unset, so `go test ./...`
// stays green in environments without a DB.
package isolation_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/db"
	"github.com/ishaangarg9/statusflow/internal/tenancy"
)

type seed struct {
	orgA, orgB         uuid.UUID
	userA, userB       uuid.UUID
	monitorA, monitorB uuid.UUID
}

func setup(t *testing.T) (context.Context, *pgxpool.Pool, seed) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping RLS isolation proof (needs real Postgres as app_user)")
	}
	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("connect as app_user: %v", err)
	}
	t.Cleanup(pool.Close)

	suffix := uuid.NewString()[:8]
	s := seed{
		orgA: uuid.New(), orgB: uuid.New(),
		userA: uuid.New(), userB: uuid.New(),
		monitorA: uuid.New(), monitorB: uuid.New(),
	}

	// Users are global (no RLS) — insert on the bare pool.
	emails := []string{"iso-" + suffix + "-a@example.com", "iso-" + suffix + "-b@example.com"}
	for i, u := range []uuid.UUID{s.userA, s.userB} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'x')`,
			u, emails[i]); err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}

	seedOrg := func(org, user, monitor uuid.UUID, slug string) {
		err := tenancy.WithOrgTx(ctx, pool, org, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx,
				`INSERT INTO organizations (id, name, slug) VALUES ($1, $2, $3)`,
				org, slug, slug); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO memberships (org_id, user_id, role) VALUES ($1, $2, 'owner')`,
				org, user); err != nil {
				return err
			}
			_, err := tx.Exec(ctx,
				`INSERT INTO monitors (id, org_id, name, url) VALUES ($1, $2, 'm', 'https://x.test')`,
				monitor, org)
			return err
		})
		if err != nil {
			t.Fatalf("seed org %s: %v", slug, err)
		}
	}
	seedOrg(s.orgA, s.userA, s.monitorA, "iso-a-"+suffix)
	seedOrg(s.orgB, s.userB, s.monitorB, "iso-b-"+suffix)

	// Cascade-cleanup both orgs (and their monitors/memberships) afterwards.
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, org := range []uuid.UUID{s.orgA, s.orgB} {
			_ = tenancy.WithOrgTx(cctx, pool, org, func(tx pgx.Tx) error {
				_, err := tx.Exec(cctx, `DELETE FROM organizations WHERE id = $1`, org)
				return err
			})
		}
		_, _ = pool.Exec(cctx, `DELETE FROM users WHERE id = ANY($1)`,
			[]uuid.UUID{s.userA, s.userB})
	})

	return ctx, pool, s
}

// TestRLSBlocksLeakEvenWithoutAppFilter is the headline proof: inside
// WithOrgTx(orgA) we run a monitors query with NO org filter at all, and the
// database still returns only org A's rows — org B's monitor is invisible.
func TestRLSBlocksLeakEvenWithoutAppFilter(t *testing.T) {
	ctx, pool, s := setup(t)

	var count int
	err := tenancy.WithOrgTx(ctx, pool, s.orgA, func(tx pgx.Tx) error {
		// Deliberately omit "WHERE org_id = ..." — RLS is the only thing scoping this.
		return tx.QueryRow(ctx, `SELECT count(*) FROM monitors`).Scan(&count)
	})
	if err != nil {
		t.Fatalf("count monitors in orgA: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly orgA's 1 monitor visible under RLS, got %d", count)
	}

	var leaked int
	if err := tenancy.WithOrgTx(ctx, pool, s.orgA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM monitors WHERE id = $1`, s.monitorB).Scan(&leaked)
	}); err != nil {
		t.Fatalf("lookup orgB monitor from orgA: %v", err)
	}
	if leaked != 0 {
		t.Fatalf("org B's monitor leaked into org A's context")
	}
}

// TestForeignRowIsInvisible confirms a read of org B's monitor id from org A's
// context returns no row (the API layer turns this into a 404).
func TestForeignRowIsInvisible(t *testing.T) {
	ctx, pool, s := setup(t)
	err := tenancy.WithOrgTx(ctx, pool, s.orgA, func(tx pgx.Tx) error {
		var id uuid.UUID
		return tx.QueryRow(ctx, `SELECT id FROM monitors WHERE id = $1`, s.monitorB).Scan(&id)
	})
	if err == nil {
		t.Fatal("expected org B's monitor to be invisible from org A, but it was found")
	}
	if err != pgx.ErrNoRows {
		t.Fatalf("expected pgx.ErrNoRows, got %v", err)
	}
}

// TestWriteCheckBlocksCrossOrgInsert confirms WITH CHECK refuses an insert that
// tries to plant a row in another org while org A is the active tenant.
func TestWriteCheckBlocksCrossOrgInsert(t *testing.T) {
	ctx, pool, s := setup(t)
	err := tenancy.WithOrgTx(ctx, pool, s.orgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO monitors (org_id, name, url) VALUES ($1, 'evil', 'https://x.test')`,
			s.orgB)
		return err
	})
	if err == nil {
		t.Fatal("expected RLS WITH CHECK to reject inserting a row for org B from org A's context")
	}
}
