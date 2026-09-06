// DemoLogin (P15) is a password-less login shortcut, so it needs the same
// scrutiny as any auth bypass: it must be default-off, must not leak whether
// it's configured vs. unseeded (both read as 404), and — when it does succeed
// — must resolve to a real, revocable session for the exact seeded account.
// The viewer-only enforcement itself is proven by the existing authz matrix
// (test/authz); DemoLogin just has to hand back a session for the right user.
package sessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/demo"
	"github.com/ishaangarg9/statusflow/internal/domain/users"
	"github.com/ishaangarg9/statusflow/internal/shared"
)

func TestDemoLogin_DisabledIsNotFound(t *testing.T) {
	ctx, pool := setup(t)
	store := newStore(pool, time.Hour)
	svc := users.NewService(pool, store, false, time.Hour)

	_, _, _, err := svc.DemoLogin(ctx, "", "127.0.0.1")
	assertNotFound(t, err)
}

func TestDemoLogin_EnabledButUnseededIsNotFound(t *testing.T) {
	ctx, pool := setup(t)

	// This asserts the "seed job hasn't run yet" path, which only holds if
	// nothing has already provisioned demo.ViewerEmail in this DB (e.g. a dev
	// box where `make seed` already ran). Skip rather than delete that row —
	// it may be backing a real local demo someone is using, and deleting it
	// would cascade-drop its org membership too.
	var exists bool
	if err := pool.QueryRow(ctx,
		`SELECT true FROM users WHERE email = $1`, demo.ViewerEmail,
	).Scan(&exists); err == nil {
		t.Skip("demo.ViewerEmail is already seeded in this DB; can't exercise the unseeded path without disturbing it")
	}

	store := newStore(pool, time.Hour)
	svc := users.NewService(pool, store, true, time.Hour)

	_, _, _, err := svc.DemoLogin(ctx, "", "127.0.0.1")
	assertNotFound(t, err)
}

func TestDemoLogin_EnabledAndSeededMintsASessionForThatUser(t *testing.T) {
	ctx, pool := setup(t)
	// A demo TTL distinct from the store's own (normal-login) TTL, so this
	// also proves DemoLogin uses demoSessionTTL and not SessionTTL — the
	// short expiry is the abuse-hardening control (P15): it's what bounds how
	// many live sessions the shared demo account can accumulate, since
	// concurrent demo visitors are allowed and nothing revokes on login.
	const demoTTL = 5 * time.Minute
	store := newStore(pool, time.Hour)
	svc := users.NewService(pool, store, true, demoTTL)

	userID := seedDemoViewer(t, ctx, pool)

	raw, view, ttl, err := svc.DemoLogin(ctx, "test-agent", "127.0.0.1")
	if err != nil {
		t.Fatalf("DemoLogin: %v", err)
	}
	if view.ID != userID {
		t.Fatalf("DemoLogin returned user %s, want the seeded demo viewer %s", view.ID, userID)
	}
	if ttl != demoTTL {
		t.Fatalf("DemoLogin returned ttl %v, want demoSessionTTL %v", ttl, demoTTL)
	}

	resolved, sess, err := store.Resolve(ctx, raw)
	if err != nil {
		t.Fatalf("the minted token must resolve to a live session: %v", err)
	}
	if resolved.ID != userID {
		t.Fatalf("resolved session belongs to %s, want %s", resolved.ID, userID)
	}
	if until := time.Until(sess.ExpiresAt); until > demoTTL || until < demoTTL-time.Minute {
		t.Fatalf("session expires in %v, want ~%v (demoSessionTTL, not the store's normal TTL)", until, demoTTL)
	}
}

func assertNotFound(t *testing.T, err error) {
	t.Helper()
	appErr, ok := err.(*shared.AppError)
	if !ok || appErr.Status != 404 {
		t.Fatalf("want a 404 AppError, got %v (%T)", err, err)
	}
}

// seedDemoViewer returns the id of a user at the fixed internal/demo.ViewerEmail
// address, which is what DemoLogin looks up. email is UNIQUE, so on a dev DB
// that already ran `make seed` this reuses the existing row (and leaves it
// alone) rather than colliding with it; only a row this test itself creates
// gets cleaned up.
func seedDemoViewer(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var existing uuid.UUID
	err := pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, demo.ViewerEmail).Scan(&existing)
	if err == nil {
		return existing
	}

	id := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'x')`,
		id, demo.ViewerEmail); err != nil {
		t.Fatalf("seed demo viewer: %v", err)
	}
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = pool.Exec(cctx, `DELETE FROM users WHERE id = $1`, id)
	})
	return id
}
