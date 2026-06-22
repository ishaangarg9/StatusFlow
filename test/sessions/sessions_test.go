// Package sessions_test proves invariant §2.7: sessions are opaque, DB-backed,
// and revocable, and revocation takes effect immediately. The cookie holds only
// a random token; the server resolves it to a sessions row, so deleting/revoking
// that row makes the very next request unauthenticated (the Authn middleware
// calls Resolve, which is exactly what these tests exercise).
//
// Requires DATABASE_URL (app_user DSN); skips otherwise, like the other
// DB-backed suites:
//
//	DATABASE_URL='postgres://app_user:devpassword@localhost:5433/statusflow?sslmode=disable' \
//	  go test ./test/sessions/...
package sessions_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/auth"
	"github.com/ishaangarg9/statusflow/internal/db"
	"github.com/ishaangarg9/statusflow/internal/domain/users"
)

func setup(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping session-revocation suite (needs real Postgres as app_user)")
	}
	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("connect as app_user: %v", err)
	}
	t.Cleanup(pool.Close)
	return ctx, pool
}

// seedUser inserts a global user and registers cleanup. Sessions cascade on
// user delete, so cleaning the user clears its sessions too.
func seedUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	email := "sess-" + uuid.NewString()[:8] + "@example.com"
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'x')`,
		id, email); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = pool.Exec(cctx, `DELETE FROM users WHERE id = $1`, id)
	})
	return id
}

func newStore(pool *pgxpool.Pool, ttl time.Duration) *auth.SessionStore {
	return auth.NewSessionStore(pool, ttl, "sf_session", false)
}

// A freshly minted session resolves; revoking it makes the next Resolve miss.
func TestRevoke_TakesEffectImmediately(t *testing.T) {
	ctx, pool := setup(t)
	store := newStore(pool, time.Hour)
	uid := seedUser(t, ctx, pool)

	raw, sess, err := store.Create(ctx, uid, "test-agent", "203.0.113.7")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	u, _, err := store.Resolve(ctx, raw)
	if err != nil || u == nil || u.ID != uid {
		t.Fatalf("expected fresh session to resolve to the owner, got u=%v err=%v", u, err)
	}

	if err := store.Revoke(ctx, sess.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, _, err := store.Resolve(ctx, raw); !auth.IsNotFound(err) {
		t.Fatalf("revoked session must not resolve; got err=%v", err)
	}
}

// "Log out everywhere": every session for the user stops resolving at once.
func TestRevokeAllForUser(t *testing.T) {
	ctx, pool := setup(t)
	store := newStore(pool, time.Hour)
	uid := seedUser(t, ctx, pool)

	rawA, _, err := store.Create(ctx, uid, "device-a", "203.0.113.1")
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	rawB, _, err := store.Create(ctx, uid, "device-b", "203.0.113.2")
	if err != nil {
		t.Fatalf("create B: %v", err)
	}

	if err := store.RevokeAllForUser(ctx, uid); err != nil {
		t.Fatalf("revoke all: %v", err)
	}
	if _, _, err := store.Resolve(ctx, rawA); !auth.IsNotFound(err) {
		t.Fatalf("device A should be revoked; got %v", err)
	}
	if _, _, err := store.Resolve(ctx, rawB); !auth.IsNotFound(err) {
		t.Fatalf("device B should be revoked; got %v", err)
	}
}

// An expired session is never resolved even though the row still exists.
func TestExpiredSessionDoesNotResolve(t *testing.T) {
	ctx, pool := setup(t)
	store := newStore(pool, -time.Minute) // expires in the past
	uid := seedUser(t, ctx, pool)

	raw, _, err := store.Create(ctx, uid, "stale", "203.0.113.9")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := store.Resolve(ctx, raw); !auth.IsNotFound(err) {
		t.Fatalf("expired session must not resolve; got %v", err)
	}
}

// The user-facing RevokeSession is scoped by user_id: you cannot revoke another
// user's session even by guessing its id — it reads as a 404 (NotFound).
func TestRevokeSession_ScopedToOwner(t *testing.T) {
	ctx, pool := setup(t)
	store := newStore(pool, time.Hour)
	svc := users.NewService(pool, store)

	victim := seedUser(t, ctx, pool)
	attacker := seedUser(t, ctx, pool)

	rawVictim, victimSess, err := store.Create(ctx, victim, "victim-dev", "203.0.113.3")
	if err != nil {
		t.Fatalf("create victim session: %v", err)
	}

	// Attacker tries to kill the victim's session by id → must be a NotFound.
	if err := svc.RevokeSession(ctx, attacker, victimSess.ID); err == nil {
		t.Fatal("attacker must not revoke another user's session")
	}
	// And the victim's session is still alive.
	if _, _, err := store.Resolve(ctx, rawVictim); err != nil {
		t.Fatalf("victim session must survive the cross-user revoke attempt; got %v", err)
	}

	// The owner can revoke it, and it takes effect immediately.
	if err := svc.RevokeSession(ctx, victim, victimSess.ID); err != nil {
		t.Fatalf("owner revoke: %v", err)
	}
	if _, _, err := store.Resolve(ctx, rawVictim); !auth.IsNotFound(err) {
		t.Fatalf("owner-revoked session must not resolve; got %v", err)
	}
}
