package worker

import (
	"context"

	"github.com/google/uuid"
)

// Monitor is the slim projection the worker needs per check.
type Monitor struct {
	ID              uuid.UUID
	OrgID           uuid.UUID
	URL             string
	Method          string
	TimeoutMs       int
	IntervalSeconds int
	ExpectedStatus  int
}

// claimDue selects up to `limit` due monitors and locks them for this tick.
// FOR UPDATE SKIP LOCKED makes N worker instances safe to run concurrently.
//
// NOTE on RLS: this is a deliberately cross-tenant scan, so it cannot be
// constrained by `app.current_org_id`. There are three ways to make it work
// alongside the strict app_user role:
//
//  1. Wrap the SELECT in a SECURITY DEFINER function (preferred): the
//     function is owned by a role that bypasses RLS; app_user is GRANTed
//     EXECUTE on it. Keeps app_user otherwise unprivileged.
//  2. Run the worker as a separate worker_user role with BYPASSRLS.
//  3. Add a permissive policy keyed on a worker-only setting.
//
// Pick (1) when adding a migration; until then this scaffold leaves the
// raw SELECT in place so the shape from doc 02 §8.2 is visible.
func (w *Worker) claimDue(ctx context.Context, limit int) ([]Monitor, error) {
	const claimSQL = `
		SELECT id, org_id, url, method, timeout_ms, interval_seconds, expected_status
		FROM monitors
		WHERE is_paused = false AND next_check_at <= now()
		ORDER BY next_check_at
		LIMIT $1
		FOR UPDATE SKIP LOCKED`

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, claimSQL, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Monitor
	for rows.Next() {
		var m Monitor
		if err := rows.Scan(&m.ID, &m.OrgID, &m.URL, &m.Method, &m.TimeoutMs, &m.IntervalSeconds, &m.ExpectedStatus); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}
