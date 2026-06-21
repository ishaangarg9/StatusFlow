// Package invitations_test is the end-to-end regression for Phase 4 (invites).
// It drives the real invitations.Service against a real Postgres as app_user and
// proves the full lifecycle: issue -> accept -> join, plus the guards that keep
// the flow safe (email binding, expiry, single-use, owner-role rejection, and
// the already-a-member conflict).
//
// Requires DATABASE_URL (app_user DSN); skips otherwise, like the isolation and
// roles suites:
//
//	DATABASE_URL='postgres://app_user:devpassword@localhost:5433/statusflow?sslmode=disable' \
//	  go test ./test/invitations/...
package invitations_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/auth"
	"github.com/ishaangarg9/statusflow/internal/authz"
	"github.com/ishaangarg9/statusflow/internal/db"
	"github.com/ishaangarg9/statusflow/internal/domain/invitations"
	"github.com/ishaangarg9/statusflow/internal/shared"
	"github.com/ishaangarg9/statusflow/internal/tenancy"
)

type fixture struct {
	pool       *pgxpool.Pool
	svc        *invitations.Service
	org        uuid.UUID
	owner      uuid.UUID // alice, owner of org
	invitee    *auth.User
	inviteeEml string
}

func setup(t *testing.T) (context.Context, fixture) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping invitations regression (needs real Postgres as app_user)")
	}
	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("connect as app_user: %v", err)
	}
	t.Cleanup(pool.Close)

	suffix := uuid.NewString()[:8]
	ownerEml := "inv-" + suffix + "-alice@example.com"
	inviteeEml := "inv-" + suffix + "-grace@example.com"
	f := fixture{
		pool:       pool,
		svc:        invitations.NewService(pool),
		org:        uuid.New(),
		owner:      uuid.New(),
		invitee:    &auth.User{ID: uuid.New(), Email: inviteeEml},
		inviteeEml: inviteeEml,
	}

	for _, u := range []struct {
		id    uuid.UUID
		email string
	}{{f.owner, ownerEml}, {f.invitee.ID, inviteeEml}} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'x')`,
			u.id, u.email); err != nil {
			t.Fatalf("seed user: %v", err)
		}
	}

	err = tenancy.WithOrgTx(ctx, pool, f.org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO organizations (id, name, slug) VALUES ($1, $2, $3)`,
			f.org, "inv-"+suffix, "inv-"+suffix); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO memberships (org_id, user_id, role) VALUES ($1, $2, 'owner')`,
			f.org, f.owner)
		return err
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
			[]uuid.UUID{f.owner, f.invitee.ID})
	})

	return ctx, f
}

func (f fixture) ownerAC() authz.AuthContext {
	return authz.AuthContext{UserID: f.owner, OrgID: f.org, Role: authz.RoleOwner}
}

func (f fixture) roleOf(ctx context.Context, t *testing.T, user uuid.UUID) (string, bool) {
	t.Helper()
	var r string
	err := tenancy.WithOrgTx(ctx, f.pool, f.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT role FROM memberships WHERE org_id = $1 AND user_id = $2`,
			f.org, user).Scan(&r)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false
	}
	if err != nil {
		t.Fatalf("role of %s: %v", user, err)
	}
	return r, true
}

func statusOf(err error) int {
	var ae *shared.AppError
	if errors.As(err, &ae) {
		return ae.Status
	}
	return 0
}

// The happy path: owner invites grace; grace accepts with the matching email and
// joins as a member. The invitation is marked accepted and no longer pending.
func TestInviteAcceptHappyPath(t *testing.T) {
	ctx, f := setup(t)

	inv, token, err := f.svc.Create(ctx, f.ownerAC(),
		invitations.CreateInput{Email: f.inviteeEml, Role: authz.RoleMember})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if token == "" {
		t.Fatal("expected a raw token to deliver")
	}
	if inv.Role != authz.RoleMember {
		t.Fatalf("invite role = %q, want member", inv.Role)
	}

	res, err := f.svc.Accept(ctx, f.invitee, token)
	if err != nil {
		t.Fatalf("accept invite: %v", err)
	}
	if res.OrgID != f.org || res.Role != authz.RoleMember {
		t.Fatalf("accept result = %+v, want org=%s role=member", res, f.org)
	}
	if role, ok := f.roleOf(ctx, t, f.invitee.ID); !ok || role != "member" {
		t.Fatalf("invitee membership = (%q,%v), want member,true", role, ok)
	}

	// No longer pending.
	pending, err := f.svc.List(ctx, f.org)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, p := range pending {
		if p.ID == inv.ID {
			t.Fatal("accepted invite should not appear in pending list")
		}
	}
}

// A valid token cannot be redeemed by a different account: acceptance is bound
// to the email the invite was issued to.
func TestAcceptRejectsEmailMismatch(t *testing.T) {
	ctx, f := setup(t)

	_, token, err := f.svc.Create(ctx, f.ownerAC(),
		invitations.CreateInput{Email: f.inviteeEml, Role: authz.RoleMember})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}

	intruder := &auth.User{ID: uuid.New(), Email: "someone-else@example.com"}
	_, err = f.svc.Accept(ctx, intruder, token)
	if status := statusOf(err); status != 403 {
		t.Fatalf("email-mismatch accept = %d (%v), want 403", status, err)
	}
	if _, ok := f.roleOf(ctx, t, intruder.ID); ok {
		t.Fatal("intruder must not have been joined")
	}
}

// An expired invitation cannot be accepted (422).
func TestAcceptRejectsExpired(t *testing.T) {
	ctx, f := setup(t)

	_, token, err := f.svc.Create(ctx, f.ownerAC(),
		invitations.CreateInput{Email: f.inviteeEml, Role: authz.RoleMember})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	// Force expiry in the past.
	tokenHash := auth.HashToken(token)
	if err := tenancy.WithOrgTx(ctx, f.pool, f.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE invitations SET expires_at = now() - interval '1 hour' WHERE org_id = $1 AND token_hash = $2`,
			f.org, tokenHash)
		return err
	}); err != nil {
		t.Fatalf("expire invite: %v", err)
	}

	_, err = f.svc.Accept(ctx, f.invitee, token)
	if status := statusOf(err); status != 422 {
		t.Fatalf("expired accept = %d (%v), want 422", status, err)
	}
	if _, ok := f.roleOf(ctx, t, f.invitee.ID); ok {
		t.Fatal("invitee must not have joined via an expired invite")
	}
}

// An invite is single-use: a second accept with the same token is a 409.
func TestAcceptIsSingleUse(t *testing.T) {
	ctx, f := setup(t)

	_, token, err := f.svc.Create(ctx, f.ownerAC(),
		invitations.CreateInput{Email: f.inviteeEml, Role: authz.RoleMember})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if _, err := f.svc.Accept(ctx, f.invitee, token); err != nil {
		t.Fatalf("first accept: %v", err)
	}
	_, err = f.svc.Accept(ctx, f.invitee, token)
	if status := statusOf(err); status != 409 {
		t.Fatalf("second accept = %d (%v), want 409", status, err)
	}
}

// An unknown/garbage token is indistinguishable from an expired one: 422.
func TestAcceptRejectsUnknownToken(t *testing.T) {
	ctx, f := setup(t)

	_, err := f.svc.Accept(ctx, f.invitee, "not-a-real-token")
	if status := statusOf(err); status != 422 {
		t.Fatalf("unknown token accept = %d (%v), want 422", status, err)
	}
}

// Inviting at the owner role is rejected (422): ownership is transfer-only and
// must never be minted via an invite.
func TestCannotInviteOwner(t *testing.T) {
	ctx, f := setup(t)

	_, _, err := f.svc.Create(ctx, f.ownerAC(),
		invitations.CreateInput{Email: f.inviteeEml, Role: authz.RoleOwner})
	if status := statusOf(err); status != 422 {
		t.Fatalf("invite-owner = %d (%v), want 422", status, err)
	}
}

// Inviting an email that already belongs to a member is a 409.
func TestCannotInviteExistingMember(t *testing.T) {
	ctx, f := setup(t)

	// owner's own email is already a member.
	var ownerEmail string
	if err := f.pool.QueryRow(ctx,
		`SELECT email FROM users WHERE id = $1`, f.owner).Scan(&ownerEmail); err != nil {
		t.Fatalf("read owner email: %v", err)
	}
	_, _, err := f.svc.Create(ctx, f.ownerAC(),
		invitations.CreateInput{Email: ownerEmail, Role: authz.RoleMember})
	if status := statusOf(err); status != 409 {
		t.Fatalf("invite-existing-member = %d (%v), want 409", status, err)
	}
}

// Re-issuing an invite for the same email replaces the pending one (resend): the
// old token stops working and the new token accepts.
func TestResendInvalidatesOldToken(t *testing.T) {
	ctx, f := setup(t)

	_, oldToken, err := f.svc.Create(ctx, f.ownerAC(),
		invitations.CreateInput{Email: f.inviteeEml, Role: authz.RoleMember})
	if err != nil {
		t.Fatalf("first invite: %v", err)
	}
	_, newToken, err := f.svc.Create(ctx, f.ownerAC(),
		invitations.CreateInput{Email: f.inviteeEml, Role: authz.RoleViewer})
	if err != nil {
		t.Fatalf("resend invite: %v", err)
	}
	if oldToken == newToken {
		t.Fatal("resend should mint a fresh token")
	}

	if _, err := f.svc.Accept(ctx, f.invitee, oldToken); statusOf(err) != 422 {
		t.Fatalf("old token after resend = %v, want 422", err)
	}
	res, err := f.svc.Accept(ctx, f.invitee, newToken)
	if err != nil {
		t.Fatalf("new token accept: %v", err)
	}
	if res.Role != authz.RoleViewer {
		t.Fatalf("joined role = %q, want viewer (the resent role)", res.Role)
	}
}

// Revoking a pending invite removes it; a subsequent accept is then a 422.
func TestRevokePreventsAccept(t *testing.T) {
	ctx, f := setup(t)

	inv, token, err := f.svc.Create(ctx, f.ownerAC(),
		invitations.CreateInput{Email: f.inviteeEml, Role: authz.RoleMember})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if err := f.svc.Revoke(ctx, f.ownerAC(), inv.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := f.svc.Accept(ctx, f.invitee, token); statusOf(err) != 422 {
		t.Fatalf("accept after revoke = %v, want 422", err)
	}
	// Revoking again is a 404.
	if err := f.svc.Revoke(ctx, f.ownerAC(), inv.ID); statusOf(err) != 404 {
		t.Fatalf("double revoke = %v, want 404", err)
	}
}
