// Package audit_test exercises the audit read API: keyset pagination walks the
// whole log exactly once with no gaps or duplicates, the action/actor filters
// narrow correctly, and a malformed cursor is a 422. Writes go through the same
// audit.Record path the domains use, inside WithOrgTx with RLS armed.
//
// Requires DATABASE_URL (app_user DSN); skips otherwise, like the other
// DB-backed suites:
//
//	DATABASE_URL='postgres://app_user:devpassword@localhost:5433/statusflow?sslmode=disable' \
//	  go test ./test/audit/...
package audit_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/db"
	"github.com/ishaangarg9/statusflow/internal/domain/audit"
	"github.com/ishaangarg9/statusflow/internal/shared"
	"github.com/ishaangarg9/statusflow/internal/tenancy"
)

type fixture struct {
	pool *pgxpool.Pool
	org  uuid.UUID
	user uuid.UUID
	svc  *audit.Service
}

func setup(t *testing.T) (context.Context, fixture) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping audit suite (needs real Postgres as app_user)")
	}
	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("connect as app_user: %v", err)
	}
	t.Cleanup(pool.Close)

	suffix := uuid.NewString()[:8]
	f := fixture{pool: pool, org: uuid.New(), user: uuid.New(), svc: audit.NewService(pool)}

	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'x')`,
		f.user, "audit-"+suffix+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := tenancy.WithOrgTx(ctx, pool, f.org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO organizations (id, name, slug) VALUES ($1, $2, $3)`,
			f.org, "audit-"+suffix, "audit-"+suffix); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO memberships (org_id, user_id, role) VALUES ($1, $2, 'owner')`,
			f.org, f.user)
		return err
	}); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = tenancy.WithOrgTx(cctx, pool, f.org, func(tx pgx.Tx) error {
			_, err := tx.Exec(cctx, `DELETE FROM organizations WHERE id = $1`, f.org)
			return err
		})
		_, _ = pool.Exec(cctx, `DELETE FROM users WHERE id = $1`, f.user)
	})
	return ctx, f
}

// write records one audit row through the production Record path.
func (f fixture) write(ctx context.Context, t *testing.T, action string, actor uuid.UUID) {
	t.Helper()
	err := tenancy.WithOrgTx(ctx, f.pool, f.org, func(tx pgx.Tx) error {
		return audit.Record(ctx, tx, audit.Entry{
			OrgID:       f.org,
			ActorUserID: actor, // uuid.Nil => system actor
			Action:      action,
		})
	})
	if err != nil {
		t.Fatalf("write audit row %q: %v", action, err)
	}
}

func TestAuditPagination_WalksAllRowsOnce(t *testing.T) {
	ctx, f := setup(t)
	const n = 5
	for range n {
		f.write(ctx, t, "monitor:create", f.user)
	}

	seen := map[uuid.UUID]bool{}
	var prev *audit.EntryView
	cursor := ""
	pages := 0
	for {
		page, err := f.svc.List(ctx, f.org, audit.ListParams{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("list page %d: %v", pages, err)
		}
		pages++
		for i := range page.Entries {
			e := page.Entries[i]
			if seen[e.ID] {
				t.Fatalf("row %s returned twice across pages", e.ID)
			}
			seen[e.ID] = true
			// Strictly descending by (created_at, id) — no overlap, no reordering.
			if prev != nil {
				if e.CreatedAt.After(prev.CreatedAt) ||
					(e.CreatedAt.Equal(prev.CreatedAt) && e.ID.String() >= prev.ID.String()) {
					t.Fatalf("ordering violated: %v/%s after %v/%s", e.CreatedAt, e.ID, prev.CreatedAt, prev.ID)
				}
			}
			ev := e
			prev = &ev
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
		if pages > n+2 {
			t.Fatal("pagination did not terminate")
		}
	}
	if len(seen) != n {
		t.Fatalf("expected to page through %d rows, saw %d", n, len(seen))
	}
}

func TestAuditFilter_Action(t *testing.T) {
	ctx, f := setup(t)
	f.write(ctx, t, "monitor:create", f.user)
	f.write(ctx, t, "monitor:create", f.user)
	f.write(ctx, t, "monitor:delete", f.user)
	f.write(ctx, t, "monitor:delete", f.user)
	f.write(ctx, t, "monitor:delete", f.user)

	page, err := f.svc.List(ctx, f.org, audit.ListParams{Action: "monitor:delete"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Entries) != 3 {
		t.Fatalf("expected 3 monitor:delete rows, got %d", len(page.Entries))
	}
	for _, e := range page.Entries {
		if e.Action != "monitor:delete" {
			t.Fatalf("action filter leaked %q", e.Action)
		}
	}
}

func TestAuditFilter_Actor(t *testing.T) {
	ctx, f := setup(t)
	f.write(ctx, t, "monitor:create", f.user)  // human actor
	f.write(ctx, t, "incident:open", uuid.Nil) // system actor
	f.write(ctx, t, "incident:resolve", uuid.Nil)

	page, err := f.svc.List(ctx, f.org, audit.ListParams{Actor: &f.user})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 row for the human actor, got %d", len(page.Entries))
	}
	if page.Entries[0].Actor == nil {
		t.Fatal("expected the human actor's email to be populated")
	}
}

func TestAuditInvalidCursor(t *testing.T) {
	ctx, f := setup(t)
	_, err := f.svc.List(ctx, f.org, audit.ListParams{Cursor: "not-a-valid-cursor!!"})
	var ae *shared.AppError
	if !errors.As(err, &ae) || ae.Status != 422 {
		t.Fatalf("expected 422 for a malformed cursor, got %v", err)
	}
}

// Retention is privileged-only: app_user must NOT be able to invoke the
// cross-tenant prune. This is the §2 boundary in action — the prune can delete
// any org's rows, so the app role is denied EXECUTE.
func TestPruneNotExecutableByAppUser(t *testing.T) {
	ctx, f := setup(t)
	_, err := f.pool.Exec(ctx, `SELECT prune_audit_logs(interval '365 days')`)
	if err == nil {
		t.Fatal("app_user must NOT be able to execute prune_audit_logs")
	}
}

// On the privileged connection the prune deletes rows older than the retention
// window and keeps newer ones; a non-positive interval is rejected. Skips
// unless MIGRATIONS_DATABASE_URL is set (the privileged DSN).
func TestPruneByPrivilegedRole(t *testing.T) {
	ctx, f := setup(t)
	mdsn := os.Getenv("MIGRATIONS_DATABASE_URL")
	if mdsn == "" {
		t.Skip("MIGRATIONS_DATABASE_URL not set; skipping privileged prune behavior test")
	}
	priv, err := pgxpool.New(ctx, mdsn)
	if err != nil {
		t.Fatalf("connect privileged: %v", err)
	}
	defer priv.Close()

	// Backdate one row well past the window and add a fresh one, both for f.org.
	// The privileged role bypasses RLS, so it inserts directly.
	var oldID, newID uuid.UUID
	if err := priv.QueryRow(ctx, `
		INSERT INTO audit_logs (org_id, action, created_at)
		VALUES ($1, 'monitor:create', now() - interval '400 days') RETURNING id`,
		f.org).Scan(&oldID); err != nil {
		t.Fatalf("seed old row: %v", err)
	}
	if err := priv.QueryRow(ctx, `
		INSERT INTO audit_logs (org_id, action) VALUES ($1, 'monitor:create') RETURNING id`,
		f.org).Scan(&newID); err != nil {
		t.Fatalf("seed new row: %v", err)
	}

	if _, err := priv.Exec(ctx, `SELECT prune_audit_logs(interval '365 days')`); err != nil {
		t.Fatalf("prune: %v", err)
	}

	exists := func(id uuid.UUID) bool {
		var n int
		if err := priv.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE id = $1`, id).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n > 0
	}
	if exists(oldID) {
		t.Fatal("400-day-old row should have been pruned")
	}
	if !exists(newID) {
		t.Fatal("fresh row must survive a 365-day retention prune")
	}

	if _, err := priv.Exec(ctx, `SELECT prune_audit_logs(interval '0')`); err == nil {
		t.Fatal("a non-positive retention interval must be rejected")
	}
}
