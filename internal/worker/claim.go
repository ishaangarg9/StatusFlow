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

// claimDue claims up to `limit` due monitors for this tick. The cross-tenant
// scan + claim + reschedule is done by the claim_due_monitors() SECURITY DEFINER
// function (migration 012): app_user has no BYPASSRLS, and this is a deliberately
// cross-org read, so the privileged-but-locked-down function is the sanctioned
// escape hatch (same pattern as user_memberships / invitation_org_by_token /
// public_status_page_org).
//
// The function atomically pushes each claimed monitor's next_check_at into the
// future, so a row is invisible to a concurrent worker the moment it is claimed.
// FOR UPDATE SKIP LOCKED inside the function guarantees N instances never claim
// the same monitor. It is a single auto-committed statement here — no explicit
// transaction, no lock held across the (potentially slow) HTTP check.
func (w *Worker) claimDue(ctx context.Context, limit int) ([]Monitor, error) {
	rows, err := w.pool.Query(ctx, `
		SELECT id, org_id, url, method, timeout_ms, interval_seconds, expected_status
		FROM claim_due_monitors($1)`, limit)
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
	return out, rows.Err()
}
