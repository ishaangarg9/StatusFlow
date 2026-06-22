// Package invitations_test is the end-to-end regression for Phase 4 (invites).
// It drives the real invitations.Service against a real Postgres as app_user and
// proves the full lifecycle: issue -> accept -> join, the guards that keep the
// flow safe (email binding, expiry, single-use, owner-role rejection, the
// already-a-member conflict, accepted-invite revoke protection), and the
// member:invite authorization gate on the management endpoints.
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
	"log/slog"
	"os"
	"strings"
	"sync"
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

// captureMailer is the test transport: it records the raw token per recipient so
// tests can drive Accept exactly as a real invitee would after receiving the
// email. Delivery is asynchronous now — Create only enqueues; the worker's
// Deliverer mints the token and calls this transport. The harness drains the
// outbox (f.deliver) after each Create to capture the token the worker would
// have mailed.
type captureMailer struct {
	mu   sync.Mutex
	sent map[string]string // lower(email) -> last raw token
}

func (m *captureMailer) SendInvitation(_ context.Context, inv invitations.Invite) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sent == nil {
		m.sent = map[string]string{}
	}
	m.sent[strings.ToLower(inv.To)] = inv.RawToken
	return nil
}

func (m *captureMailer) tokenFor(email string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sent[strings.ToLower(email)]
}

type fixture struct {
	pool       *pgxpool.Pool
	svc        *invitations.Service
	deliverer  *invitations.Deliverer
	mail       *captureMailer
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
	mail := &captureMailer{}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	f := fixture{
		pool:       pool,
		svc:        invitations.NewService(pool),
		deliverer:  invitations.NewDeliverer(pool, mail, log),
		mail:       mail,
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

// invite issues an invitation as the owner, drains the delivery outbox (the
// worker's job), and returns the view plus the raw token the transport received.
func (f fixture) invite(ctx context.Context, t *testing.T, email string, role authz.Role) (*invitations.InvitationView, string) {
	t.Helper()
	inv, err := f.svc.Create(ctx, f.ownerAC(), invitations.CreateInput{Email: email, Role: role})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if _, err := f.deliverer.DrainOnce(ctx, 50, 60); err != nil {
		t.Fatalf("drain outbox: %v", err)
	}
	token := f.mail.tokenFor(email)
	if token == "" {
		t.Fatal("mailer did not receive a raw token after draining the outbox")
	}
	return inv, token
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

	inv, token := f.invite(ctx, t, f.inviteeEml, authz.RoleMember)
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
	pending, err := f.svc.List(ctx, f.ownerAC())
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

	_, token := f.invite(ctx, t, f.inviteeEml, authz.RoleMember)

	intruder := &auth.User{ID: uuid.New(), Email: "someone-else@example.com"}
	_, err := f.svc.Accept(ctx, intruder, token)
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

	_, token := f.invite(ctx, t, f.inviteeEml, authz.RoleMember)
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

	_, err := f.svc.Accept(ctx, f.invitee, token)
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

	_, token := f.invite(ctx, t, f.inviteeEml, authz.RoleMember)
	if _, err := f.svc.Accept(ctx, f.invitee, token); err != nil {
		t.Fatalf("first accept: %v", err)
	}
	_, err := f.svc.Accept(ctx, f.invitee, token)
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

	_, err := f.svc.Create(ctx, f.ownerAC(),
		invitations.CreateInput{Email: f.inviteeEml, Role: authz.RoleOwner})
	if status := statusOf(err); status != 422 {
		t.Fatalf("invite-owner = %d (%v), want 422", status, err)
	}
}

// Inviting an email that already belongs to a member is a 409.
func TestCannotInviteExistingMember(t *testing.T) {
	ctx, f := setup(t)

	var ownerEmail string
	if err := f.pool.QueryRow(ctx,
		`SELECT email FROM users WHERE id = $1`, f.owner).Scan(&ownerEmail); err != nil {
		t.Fatalf("read owner email: %v", err)
	}
	_, err := f.svc.Create(ctx, f.ownerAC(),
		invitations.CreateInput{Email: ownerEmail, Role: authz.RoleMember})
	if status := statusOf(err); status != 409 {
		t.Fatalf("invite-existing-member = %d (%v), want 409", status, err)
	}
}

// Re-issuing an invite for the same email replaces the pending one (resend): the
// old token stops working and the new token accepts with the resent role.
func TestResendInvalidatesOldToken(t *testing.T) {
	ctx, f := setup(t)

	_, oldToken := f.invite(ctx, t, f.inviteeEml, authz.RoleMember)
	_, newToken := f.invite(ctx, t, f.inviteeEml, authz.RoleViewer)
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

// Revoking a pending invite removes it; a subsequent accept is then a 422, and a
// repeat revoke is a 404.
func TestRevokePreventsAccept(t *testing.T) {
	ctx, f := setup(t)

	inv, token := f.invite(ctx, t, f.inviteeEml, authz.RoleMember)
	if err := f.svc.Revoke(ctx, f.ownerAC(), inv.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := f.svc.Accept(ctx, f.invitee, token); statusOf(err) != 422 {
		t.Fatalf("accept after revoke = %v, want 422", err)
	}
	if err := f.svc.Revoke(ctx, f.ownerAC(), inv.ID); statusOf(err) != 404 {
		t.Fatalf("double revoke = %v, want 404", err)
	}
}

// An ACCEPTED invitation cannot be revoked: revoke only touches pending invites,
// so the historical accepted row survives (404 on the revoke attempt).
func TestRevokeCannotDeleteAcceptedInvite(t *testing.T) {
	ctx, f := setup(t)

	inv, token := f.invite(ctx, t, f.inviteeEml, authz.RoleMember)
	if _, err := f.svc.Accept(ctx, f.invitee, token); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if err := f.svc.Revoke(ctx, f.ownerAC(), inv.ID); statusOf(err) != 404 {
		t.Fatalf("revoke accepted invite = %v, want 404", err)
	}
	// The accepted row must still exist.
	var n int
	if err := tenancy.WithOrgTx(ctx, f.pool, f.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM invitations WHERE org_id = $1 AND id = $2 AND accepted_at IS NOT NULL`,
			f.org, inv.ID).Scan(&n)
	}); err != nil {
		t.Fatalf("count accepted invite: %v", err)
	}
	if n != 1 {
		t.Fatalf("accepted invitation row was destroyed (found %d, want 1)", n)
	}
}

// The three management endpoints are gated on member:invite (owner/admin only):
// a member or viewer is forbidden, and no DB work happens (403 before the tx).
func TestManagementEndpointsDenyLowRoles(t *testing.T) {
	ctx, f := setup(t)

	for _, role := range []authz.Role{authz.RoleMember, authz.RoleViewer} {
		ac := authz.AuthContext{UserID: uuid.New(), OrgID: f.org, Role: role}

		if _, err := f.svc.Create(ctx, ac,
			invitations.CreateInput{Email: f.inviteeEml, Role: authz.RoleMember}); statusOf(err) != 403 {
			t.Errorf("%s create = %v, want 403", role, err)
		}
		if _, err := f.svc.List(ctx, ac); statusOf(err) != 403 {
			t.Errorf("%s list = %v, want 403", role, err)
		}
		if err := f.svc.Revoke(ctx, ac, uuid.New()); statusOf(err) != 403 {
			t.Errorf("%s revoke = %v, want 403", role, err)
		}
	}
}

// Delivery is asynchronous and tokenless-at-rest: Create enqueues an outbox row
// and leaves token_hash NULL (nothing secret persisted); draining mints the
// token, stores only its hash (matching the mailed raw token), and removes the
// outbox row.
func TestCreateEnqueuesAndDrainDelivers(t *testing.T) {
	ctx, f := setup(t)

	inv, err := f.svc.Create(ctx, f.ownerAC(),
		invitations.CreateInput{Email: f.inviteeEml, Role: authz.RoleMember})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Pre-drain: no token yet, exactly one queued delivery.
	hash, outbox := f.inviteState(ctx, t, inv.ID)
	if hash != nil {
		t.Fatal("token_hash must be NULL before delivery (no secret at rest)")
	}
	if outbox != 1 {
		t.Fatalf("expected 1 queued outbox row, got %d", outbox)
	}
	if f.mail.tokenFor(f.inviteeEml) != "" {
		t.Fatal("nothing should have been mailed before draining")
	}

	if _, err := f.deliverer.DrainOnce(ctx, 50, 60); err != nil {
		t.Fatalf("drain: %v", err)
	}

	// Post-drain: token mailed, its hash stored, outbox row gone.
	token := f.mail.tokenFor(f.inviteeEml)
	if token == "" {
		t.Fatal("expected a token to be mailed after draining")
	}
	hash, outbox = f.inviteState(ctx, t, inv.ID)
	if hash == nil || *hash != auth.HashToken(token) {
		t.Fatal("stored token_hash must match the mailed token's hash")
	}
	if outbox != 0 {
		t.Fatalf("outbox row should be removed after delivery, found %d", outbox)
	}
}

// inviteState returns the invitation's token_hash (nil when NULL) and the count
// of its outbox rows.
func (f fixture) inviteState(ctx context.Context, t *testing.T, id uuid.UUID) (*string, int) {
	t.Helper()
	var hash *string
	var outbox int
	if err := tenancy.WithOrgTx(ctx, f.pool, f.org, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`SELECT token_hash FROM invitations WHERE org_id = $1 AND id = $2`,
			f.org, id).Scan(&hash); err != nil {
			return err
		}
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM invitation_outbox WHERE org_id = $1 AND invitation_id = $2`,
			f.org, id).Scan(&outbox)
	}); err != nil {
		t.Fatalf("read invite state: %v", err)
	}
	return hash, outbox
}

// An accepted invitation is never re-delivered: the claim skips accepted rows
// and Accept drops any pending outbox row, so a worker can't mint a fresh, valid
// token for an org the user already joined.
func TestAcceptedInviteIsNotRedelivered(t *testing.T) {
	ctx, f := setup(t)

	inv, token := f.invite(ctx, t, f.inviteeEml, authz.RoleMember)
	if _, err := f.svc.Accept(ctx, f.invitee, token); err != nil {
		t.Fatalf("accept: %v", err)
	}

	hashAfterAccept, _ := f.inviteState(ctx, t, inv.ID)
	if hashAfterAccept == nil {
		t.Fatal("accepted invite should still carry the delivered token's hash")
	}

	// Simulate a lingering delivery (e.g. a prior deleteOutbox that failed): a
	// stray outbox row pointing at the now-accepted invite, due immediately.
	if err := tenancy.WithOrgTx(ctx, f.pool, f.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO invitation_outbox (org_id, invitation_id, next_attempt_at)
			 VALUES ($1, $2, now())`, f.org, inv.ID)
		return err
	}); err != nil {
		t.Fatalf("seed stray outbox row: %v", err)
	}

	// Draining must NOT re-mint: the claim filters out accepted invites.
	if _, err := f.deliverer.DrainOnce(ctx, 50, 60); err != nil {
		t.Fatalf("drain: %v", err)
	}
	hashAfterDrain, _ := f.inviteState(ctx, t, inv.ID)
	if hashAfterDrain == nil || *hashAfterDrain != *hashAfterAccept {
		t.Fatal("token_hash was rotated for an already-accepted invite (claim filter failed)")
	}
}

// owner and admin may manage invitations (list succeeds for both).
func TestManagementEndpointsAllowOwnerAndAdmin(t *testing.T) {
	ctx, f := setup(t)

	for _, role := range []authz.Role{authz.RoleOwner, authz.RoleAdmin} {
		ac := authz.AuthContext{UserID: f.owner, OrgID: f.org, Role: role}
		if _, err := f.svc.List(ctx, ac); err != nil {
			t.Errorf("%s list = %v, want success", role, err)
		}
	}
}
