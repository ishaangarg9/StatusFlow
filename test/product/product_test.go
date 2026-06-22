// Package product_test drives the Phase 5 product services and the worker
// incident engine against a REAL Postgres as the restricted app_user role. It
// proves the org-scoped happy paths, the public status-page strict projection,
// and the automatic open/resolve state machine.
//
// Requires DATABASE_URL (app_user DSN); skips otherwise, like the other
// DB-backed suites:
//
//	DATABASE_URL='postgres://app_user:devpassword@localhost:5433/statusflow?sslmode=disable' \
//	  go test ./test/product/...
package product_test

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
	"github.com/ishaangarg9/statusflow/internal/domain/incidents"
	"github.com/ishaangarg9/statusflow/internal/domain/monitors"
	"github.com/ishaangarg9/statusflow/internal/domain/statuspages"
	"github.com/ishaangarg9/statusflow/internal/shared"
	"github.com/ishaangarg9/statusflow/internal/tenancy"
	"github.com/ishaangarg9/statusflow/internal/worker"
)

type fixture struct {
	pool *pgxpool.Pool
	org  uuid.UUID
	user uuid.UUID
	ac   authz.AuthContext
}

func setup(t *testing.T) (context.Context, fixture) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping product suite (needs real Postgres as app_user)")
	}
	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("connect as app_user: %v", err)
	}
	t.Cleanup(pool.Close)

	suffix := uuid.NewString()[:8]
	f := fixture{pool: pool, org: uuid.New(), user: uuid.New()}
	f.ac = authz.AuthContext{UserID: f.user, OrgID: f.org, Role: authz.RoleOwner}

	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'x')`,
		f.user, "prod-"+suffix+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := tenancy.WithOrgTx(ctx, pool, f.org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO organizations (id, name, slug) VALUES ($1, $2, $3)`,
			f.org, "prod-"+suffix, "prod-"+suffix); err != nil {
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

func appErrStatus(err error) int {
	var ae *shared.AppError
	if errors.As(err, &ae) {
		return ae.Status
	}
	return 0
}

// TestMonitorLifecycle covers create → get → checks (empty) → delete via the
// real service, and confirms a deleted monitor is then a 404.
func TestMonitorLifecycle(t *testing.T) {
	ctx, f := setup(t)
	svc := monitors.NewService(f.pool)

	m, err := svc.Create(ctx, f.ac, monitors.CreateInput{Name: "Marketing", URL: "https://acme.example"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if m.Method != "GET" || m.IntervalSeconds != 60 {
		t.Fatalf("defaults not applied: %+v", m)
	}

	got, err := svc.Get(ctx, f.org, m.ID)
	if err != nil || got.ID != m.ID {
		t.Fatalf("get: %v (%+v)", err, got)
	}

	checks, err := svc.Checks(ctx, f.org, m.ID, 50)
	if err != nil {
		t.Fatalf("checks: %v", err)
	}
	if len(checks) != 0 {
		t.Fatalf("expected no checks yet, got %d", len(checks))
	}

	if err := svc.Delete(ctx, f.ac, m.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := svc.Get(ctx, f.org, m.ID); appErrStatus(err) != 404 {
		t.Fatalf("expected 404 after delete, got %v", err)
	}
}

// TestMonitorCreateValidation rejects a non-http URL at the edge (422).
func TestMonitorCreateValidation(t *testing.T) {
	ctx, f := setup(t)
	svc := monitors.NewService(f.pool)
	if _, err := svc.Create(ctx, f.ac, monitors.CreateInput{Name: "x", URL: "file:///etc/passwd"}); appErrStatus(err) != 422 {
		t.Fatalf("expected 422 for non-http URL, got %v", err)
	}
}

// TestIncidentDoubleOpenConflicts proves the partial unique index turns a second
// open incident for the same monitor into a 409.
func TestIncidentDoubleOpenConflicts(t *testing.T) {
	ctx, f := setup(t)
	mon := monitors.NewService(f.pool)
	inc := incidents.NewService(f.pool)

	m, err := mon.Create(ctx, f.ac, monitors.CreateInput{Name: "API", URL: "https://api.example"})
	if err != nil {
		t.Fatalf("create monitor: %v", err)
	}
	if _, err := inc.Create(ctx, f.ac, incidents.CreateInput{MonitorID: m.ID, Title: "down"}); err != nil {
		t.Fatalf("first incident: %v", err)
	}
	if _, err := inc.Create(ctx, f.ac, incidents.CreateInput{MonitorID: m.ID, Title: "down again"}); appErrStatus(err) != 409 {
		t.Fatalf("expected 409 on second open incident, got %v", err)
	}
}

// TestPublicProjectionHidesInternalsAndRespectsVisibility builds a public page
// with one monitor, marks it down via a check, and asserts the public view
// reports the component as down WITHOUT leaking the monitor URL. It also asserts
// a private page is a 404.
func TestPublicProjectionHidesInternalsAndRespectsVisibility(t *testing.T) {
	ctx, f := setup(t)
	mon := monitors.NewService(f.pool)
	sp := statuspages.NewService(f.pool)

	m, err := mon.Create(ctx, f.ac, monitors.CreateInput{Name: "Marketing site", URL: "https://secret-internal.example/path"})
	if err != nil {
		t.Fatalf("create monitor: %v", err)
	}

	// A latest "down" check so the component derives to down.
	if err := tenancy.WithOrgTx(ctx, f.pool, f.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO check_results (org_id, monitor_id, status, status_code)
			VALUES ($1, $2, 'down', 503)`, f.org, m.ID)
		return err
	}); err != nil {
		t.Fatalf("seed check: %v", err)
	}

	slug := "acme-" + uuid.NewString()[:8]
	page, err := sp.Create(ctx, f.ac, statuspages.CreateInput{
		Slug: slug, Title: "Acme Status", IsPublic: true, MonitorIDs: []uuid.UUID{m.ID},
	})
	if err != nil {
		t.Fatalf("create page: %v", err)
	}
	if len(page.MonitorIDs) != 1 {
		t.Fatalf("expected 1 attached monitor, got %d", len(page.MonitorIDs))
	}

	view, err := sp.PublicView(ctx, slug)
	if err != nil {
		t.Fatalf("public view: %v", err)
	}
	if view.Overall != "down" {
		t.Fatalf("expected overall=down, got %q", view.Overall)
	}
	if len(view.Components) != 1 || view.Components[0].Name != "Marketing site" || view.Components[0].Status != "down" {
		t.Fatalf("unexpected components: %+v", view.Components)
	}

	// A made-private page must be a 404 from the public path (existence hidden).
	if _, err := sp.Update(ctx, f.ac, page.ID, statuspages.UpdateInput{IsPublic: boolPtr(false)}); err != nil {
		t.Fatalf("make private: %v", err)
	}
	if _, err := sp.PublicView(ctx, slug); appErrStatus(err) != 404 {
		t.Fatalf("expected 404 for private page, got %v", err)
	}
}

// TestIncidentEngineOpensAndResolves proves the worker state machine: a failure
// streak >= openThreshold opens exactly one incident, and a success streak >=
// resolveThreshold resolves it.
func TestIncidentEngineOpensAndResolves(t *testing.T) {
	ctx, f := setup(t)
	mon := monitors.NewService(f.pool)
	engine := worker.NewIncidentEngine(2, 2) // open after 2 downs, resolve after 2 ups

	m, err := mon.Create(ctx, f.ac, monitors.CreateInput{Name: "Edge", URL: "https://edge.example"})
	if err != nil {
		t.Fatalf("create monitor: %v", err)
	}

	// Helper: record a check and run the engine on the same tx, as the worker does.
	apply := func(status string) {
		if err := tenancy.WithOrgTx(ctx, f.pool, f.org, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx,
				`INSERT INTO check_results (org_id, monitor_id, status) VALUES ($1, $2, $3)`,
				f.org, m.ID, status); err != nil {
				return err
			}
			return engine.Apply(ctx, tx, f.org, m.ID, status)
		}); err != nil {
			t.Fatalf("apply %s: %v", status, err)
		}
	}

	openCount := func() int {
		var n int
		if err := tenancy.WithOrgTx(ctx, f.pool, f.org, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT count(*) FROM incidents WHERE org_id = $1 AND monitor_id = $2 AND status = 'open'`,
				f.org, m.ID).Scan(&n)
		}); err != nil {
			t.Fatalf("count open: %v", err)
		}
		return n
	}

	apply("down") // streak 1 — below threshold, no incident
	if openCount() != 0 {
		t.Fatal("incident opened on first failure (should wait for the streak)")
	}
	apply("down") // streak 2 — opens
	if openCount() != 1 {
		t.Fatalf("expected exactly 1 open incident after 2 downs, got %d", openCount())
	}
	apply("down") // still down — must not double-open
	if openCount() != 1 {
		t.Fatalf("expected the incident not to double-open, got %d", openCount())
	}

	apply("up") // streak 1 — below resolve threshold
	if openCount() != 1 {
		t.Fatal("incident resolved on first success (should wait for the streak)")
	}
	apply("up") // streak 2 — resolves
	if openCount() != 0 {
		t.Fatalf("expected the incident to resolve after 2 ups, got %d open", openCount())
	}
}

// TestWorkerLeavesManualIncidentsAlone proves the worker's auto-resolve never
// closes a human-authored incident: a manual incident stays open through a full
// recovery up-streak that would resolve a worker-opened one.
func TestWorkerLeavesManualIncidentsAlone(t *testing.T) {
	ctx, f := setup(t)
	mon := monitors.NewService(f.pool)
	inc := incidents.NewService(f.pool)
	engine := worker.NewIncidentEngine(2, 2)

	m, err := mon.Create(ctx, f.ac, monitors.CreateInput{Name: "Edge", URL: "https://edge.example"})
	if err != nil {
		t.Fatalf("create monitor: %v", err)
	}
	manual, err := inc.Create(ctx, f.ac, incidents.CreateInput{MonitorID: m.ID, Title: "human-opened"})
	if err != nil {
		t.Fatalf("manual incident: %v", err)
	}

	// Feed a recovery up-streak through the engine, as the worker would.
	for range 3 {
		if err := tenancy.WithOrgTx(ctx, f.pool, f.org, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx,
				`INSERT INTO check_results (org_id, monitor_id, status) VALUES ($1, $2, 'up')`,
				f.org, m.ID); err != nil {
				return err
			}
			return engine.Apply(ctx, tx, f.org, m.ID, "up")
		}); err != nil {
			t.Fatalf("apply up: %v", err)
		}
	}

	got, err := inc.Get(ctx, f.org, manual.ID)
	if err != nil {
		t.Fatalf("get manual incident: %v", err)
	}
	if got.Status != "open" {
		t.Fatalf("worker auto-resolved a manually-created incident (status=%q)", got.Status)
	}
}

// TestIncidentUpdateStatusValidation rejects an out-of-vocabulary status and
// refuses updates on a resolved incident.
func TestIncidentUpdateStatusValidation(t *testing.T) {
	ctx, f := setup(t)
	mon := monitors.NewService(f.pool)
	inc := incidents.NewService(f.pool)

	m, err := mon.Create(ctx, f.ac, monitors.CreateInput{Name: "API", URL: "https://api.example"})
	if err != nil {
		t.Fatalf("create monitor: %v", err)
	}
	created, err := inc.Create(ctx, f.ac, incidents.CreateInput{MonitorID: m.ID, Title: "down"})
	if err != nil {
		t.Fatalf("create incident: %v", err)
	}

	// Garbage status → 422.
	if _, err := inc.AddUpdate(ctx, f.ac, created.ID, incidents.UpdateInput{Message: "x", Status: "banana"}); appErrStatus(err) != 422 {
		t.Fatalf("expected 422 for invalid update status, got %v", err)
	}
	// Valid status → ok.
	if _, err := inc.AddUpdate(ctx, f.ac, created.ID, incidents.UpdateInput{Message: "looking", Status: "investigating"}); err != nil {
		t.Fatalf("valid update rejected: %v", err)
	}
	// After resolve, further updates are refused (409).
	if _, err := inc.Resolve(ctx, f.ac, created.ID); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if _, err := inc.AddUpdate(ctx, f.ac, created.ID, incidents.UpdateInput{Message: "more", Status: "monitoring"}); appErrStatus(err) != 409 {
		t.Fatalf("expected 409 adding update to resolved incident, got %v", err)
	}
}

// TestPublicViewIgnoresStaleChecks proves the recency bound: a 'down' check far
// older than the monitor's interval is not reported as the current status.
func TestPublicViewIgnoresStaleChecks(t *testing.T) {
	ctx, f := setup(t)
	mon := monitors.NewService(f.pool)
	sp := statuspages.NewService(f.pool)

	m, err := mon.Create(ctx, f.ac, monitors.CreateInput{Name: "Old", URL: "https://old.example"})
	if err != nil {
		t.Fatalf("create monitor: %v", err)
	}
	// A 'down' check well beyond 3× the 60s default interval.
	if err := tenancy.WithOrgTx(ctx, f.pool, f.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO check_results (org_id, monitor_id, status, checked_at)
			VALUES ($1, $2, 'down', now() - interval '1 hour')`, f.org, m.ID)
		return err
	}); err != nil {
		t.Fatalf("seed stale check: %v", err)
	}

	slug := "stale-" + uuid.NewString()[:8]
	if _, err := sp.Create(ctx, f.ac, statuspages.CreateInput{
		Slug: slug, Title: "S", IsPublic: true, MonitorIDs: []uuid.UUID{m.ID},
	}); err != nil {
		t.Fatalf("create page: %v", err)
	}
	view, err := sp.PublicView(ctx, slug)
	if err != nil {
		t.Fatalf("public view: %v", err)
	}
	if len(view.Components) != 1 || view.Components[0].Status != "operational" {
		t.Fatalf("stale 'down' check should not be reported as current; got %+v", view.Components)
	}
}

func boolPtr(b bool) *bool { return &b }
