package worker

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/db"
	"github.com/ishaangarg9/statusflow/internal/tenancy"
)

// leaseFixture seeds one org with one due monitor and returns a Worker wired to
// the same pool. claim_due_monitors is cross-tenant, so assertions are scoped to
// this monitor's id (other concurrent test data may also be due).
type leaseFixture struct {
	pool *pgxpool.Pool
	w    *Worker
	org  uuid.UUID
	mon  uuid.UUID
}

func leaseSetup(t *testing.T) (context.Context, leaseFixture) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping worker lease suite (needs real Postgres as app_user)")
	}
	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("connect as app_user: %v", err)
	}
	t.Cleanup(pool.Close)

	f := leaseFixture{pool: pool, org: uuid.New(), mon: uuid.New()}
	f.w = New(pool, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})), Config{
		Tick: time.Second, Batch: 50, Concurrency: 1, ClaimLeaseSeconds: 90,
		IncidentOpenThreshold: 2, IncidentResolveThreshold: 2,
	})

	if err := tenancy.WithOrgTx(ctx, pool, f.org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO organizations (id, name, slug) VALUES ($1, $2, $3)`,
			f.org, "lease-"+uuid.NewString()[:8], "lease-"+uuid.NewString()[:8]); err != nil {
			return err
		}
		// Due now: interval 30s, timeout 5s → effective lease = max(90, 5+30) = 90s.
		_, err := tx.Exec(ctx, `
			INSERT INTO monitors (id, org_id, name, url, interval_seconds, timeout_ms, next_check_at)
			VALUES ($1, $2, 'lease', 'https://example.com', 30, 5000, now() - interval '1 second')`,
			f.mon, f.org)
		return err
	}); err != nil {
		t.Fatalf("seed org+monitor: %v", err)
	}
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = tenancy.WithOrgTx(cctx, pool, f.org, func(tx pgx.Tx) error {
			_, err := tx.Exec(cctx, `DELETE FROM organizations WHERE id = $1`, f.org)
			return err
		})
	})
	return ctx, f
}

// nextCheckIn returns how far the monitor's next_check_at is from now.
func (f leaseFixture) nextCheckIn(ctx context.Context, t *testing.T) time.Duration {
	t.Helper()
	var d time.Duration
	if err := tenancy.WithOrgTx(ctx, f.pool, f.org, func(tx pgx.Tx) error {
		var secs float64
		err := tx.QueryRow(ctx,
			`SELECT EXTRACT(EPOCH FROM (next_check_at - now())) FROM monitors WHERE org_id = $1 AND id = $2`,
			f.org, f.mon).Scan(&secs)
		d = time.Duration(secs * float64(time.Second))
		return err
	}); err != nil {
		t.Fatalf("read next_check_at: %v", err)
	}
	return d
}

func (f leaseFixture) setNextCheck(ctx context.Context, t *testing.T, expr string) {
	t.Helper()
	if err := tenancy.WithOrgTx(ctx, f.pool, f.org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE monitors SET next_check_at = `+expr+` WHERE org_id = $1 AND id = $2`,
			f.org, f.mon)
		return err
	}); err != nil {
		t.Fatalf("set next_check_at: %v", err)
	}
}

func contains(ms []Monitor, id uuid.UUID) bool {
	for _, m := range ms {
		if m.ID == id {
			return true
		}
	}
	return false
}

// Claiming sets a SHORT lease (not the full interval) and makes the row
// invisible to an immediate second claim — N workers never double-process it.
func TestClaimSetsLeaseAndIsExclusive(t *testing.T) {
	ctx, f := leaseSetup(t)

	first, err := f.w.claimDue(ctx, 50, 90)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if !contains(first, f.mon) {
		t.Fatal("a due monitor should be claimed on the first pass")
	}

	// Lease (~90s) must clearly exceed the 30s interval — proving the claim used
	// the lease, not the interval, and didn't reset to a full interval.
	if in := f.nextCheckIn(ctx, t); in < 60*time.Second {
		t.Fatalf("expected a lease well beyond the 30s interval, next check in %s", in)
	}

	second, err := f.w.claimDue(ctx, 50, 90)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if contains(second, f.mon) {
		t.Fatal("a leased monitor must not be re-claimed before its lease expires")
	}
}

// After a check is persisted, next_check_at is reset from the lease down to the
// real interval. (The target is loopback so the SSRF guard returns down — the
// result still persists and the reschedule still runs, which is what we assert.)
func TestRunCheckResetsToInterval(t *testing.T) {
	ctx, f := leaseSetup(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	m := Monitor{ID: f.mon, OrgID: f.org, URL: srv.URL, Method: http.MethodGet, TimeoutMs: 2000, IntervalSeconds: 30, ExpectedStatus: 200}

	// Claim sets the lease, then the check resets to the interval.
	if _, err := f.w.claimDue(ctx, 50, 90); err != nil {
		t.Fatalf("claim: %v", err)
	}
	f.w.runCheck(ctx, m)

	if in := f.nextCheckIn(ctx, t); in <= 0 || in > 35*time.Second {
		t.Fatalf("expected next check ~30s out (the interval, not the lease), got %s", in)
	}

	var n int
	if err := tenancy.WithOrgTx(ctx, f.pool, f.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM check_results WHERE org_id = $1 AND monitor_id = $2`,
			f.org, f.mon).Scan(&n)
	}); err != nil {
		t.Fatalf("count checks: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected the check result to be persisted, got %d rows", n)
	}
}

// The reaper IS the lease: a monitor whose lease has elapsed (e.g. its worker
// crashed mid-check) becomes due again and is re-claimed. Simulated by pushing
// next_check_at into the past.
func TestExpiredLeaseIsReclaimed(t *testing.T) {
	ctx, f := leaseSetup(t)

	if _, err := f.w.claimDue(ctx, 50, 90); err != nil {
		t.Fatalf("claim: %v", err)
	}
	// Simulate a crash: the lease elapsed with no successful check/reschedule.
	f.setNextCheck(ctx, t, "now() - interval '1 second'")

	again, err := f.w.claimDue(ctx, 50, 90)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if !contains(again, f.mon) {
		t.Fatal("a monitor with an expired lease must be re-claimed")
	}
}
