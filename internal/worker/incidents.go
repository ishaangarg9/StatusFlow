package worker

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ishaangarg9/statusflow/internal/domain/audit"
)

// IncidentEngine drives the per-monitor up/down state machine:
//
//	UP   --(failure streak >= openThreshold)----> DOWN  [open incident if none open]
//	DOWN --(success streak >= resolveThreshold)-> UP    [resolve the open incident]
//
// The partial unique index incidents_one_open_per_monitor guarantees at most
// one open incident per monitor under concurrency. Streak counts come from
// recent check_results inside the same WithOrgTx as Apply.
type IncidentEngine struct {
	openThreshold    int
	resolveThreshold int
}

func NewIncidentEngine(open, resolve int) *IncidentEngine {
	if open < 1 {
		open = 1
	}
	if resolve < 1 {
		resolve = 1
	}
	return &IncidentEngine{openThreshold: open, resolveThreshold: resolve}
}

// Apply runs after a check is persisted, inside the same tx. It reads the most
// recent streak for the monitor and opens or resolves an incident as needed.
// Incidents opened/resolved here are system-authored (NULL actor in the audit
// trail). All reads/writes carry an explicit org filter on top of RLS.
func (e *IncidentEngine) Apply(ctx context.Context, tx pgx.Tx, orgID, monitorID uuid.UUID, latestStatus string) error {
	var threshold int
	switch latestStatus {
	case "down":
		threshold = e.openThreshold
	case "up":
		threshold = e.resolveThreshold
	default:
		return nil // unknown status — nothing to drive
	}

	// A streak is met when the most recent `threshold` checks all share the
	// latest status. Fetching exactly `threshold` rows and requiring a full run
	// keeps this a single bounded read.
	rows, err := tx.Query(ctx, `
		SELECT status FROM check_results
		WHERE org_id = $1 AND monitor_id = $2
		ORDER BY checked_at DESC
		LIMIT $3`, orgID, monitorID, threshold)
	if err != nil {
		return err
	}
	var n, matching int
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			rows.Close()
			return err
		}
		n++
		if status == latestStatus {
			matching++
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if n < threshold || matching != threshold {
		return nil // streak not yet established
	}

	if latestStatus == "down" {
		return e.openIncident(ctx, tx, orgID, monitorID)
	}
	return e.resolveIncident(ctx, tx, orgID, monitorID)
}

// openIncident opens a worker-sourced incident for the monitor unless one is
// already open. The partial unique index makes the ON CONFLICT a no-op when ANY
// open incident exists (manual or worker), so concurrent workers can't
// double-open and the worker won't open a duplicate alongside a human-authored
// one.
func (e *IncidentEngine) openIncident(ctx context.Context, tx pgx.Tx, orgID, monitorID uuid.UUID) error {
	var incidentID uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO incidents (org_id, monitor_id, title, status, source)
		SELECT $1, $2, 'Automated: ' || m.name || ' is down', 'open', 'worker'
		FROM monitors m
		WHERE m.id = $2 AND m.org_id = $1
		ON CONFLICT (monitor_id) WHERE status = 'open' DO NOTHING
		RETURNING id`, orgID, monitorID).Scan(&incidentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // an incident was already open (or monitor vanished) — no-op
		}
		return err
	}
	return audit.Record(ctx, tx, audit.Entry{
		OrgID:        orgID,
		Action:       "incident:open", // system actor (NULL)
		ResourceType: "incident",
		ResourceID:   &incidentID,
		Metadata:     map[string]any{"monitorId": monitorID.String(), "source": "worker"},
	})
}

// resolveIncident closes the monitor's open WORKER-sourced incident, if any. It
// deliberately leaves manually-opened incidents (source='manual') untouched: a
// human-authored incident must be resolved by a human, never silently by the
// monitor recovering.
func (e *IncidentEngine) resolveIncident(ctx context.Context, tx pgx.Tx, orgID, monitorID uuid.UUID) error {
	var incidentID uuid.UUID
	err := tx.QueryRow(ctx, `
		UPDATE incidents SET status = 'resolved', resolved_at = now()
		WHERE org_id = $1 AND monitor_id = $2 AND status = 'open' AND source = 'worker'
		RETURNING id`, orgID, monitorID).Scan(&incidentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // nothing worker-opened to resolve — no-op
		}
		return err
	}
	return audit.Record(ctx, tx, audit.Entry{
		OrgID:        orgID,
		Action:       "incident:resolve", // system actor (NULL)
		ResourceType: "incident",
		ResourceID:   &incidentID,
		Metadata:     map[string]any{"monitorId": monitorID.String(), "source": "worker"},
	})
}
