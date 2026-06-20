package worker

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	return &IncidentEngine{openThreshold: open, resolveThreshold: resolve}
}

// Apply runs after a check is persisted, inside the same tx. It reads the
// recent streak for the monitor and opens or resolves an incident as needed.
//
// Skeleton — see CLAUDE.md §5 and doc 02 §8.4 for the full recipe.
func (e *IncidentEngine) Apply(ctx context.Context, tx pgx.Tx, orgID, monitorID uuid.UUID, latestStatus string) error {
	_ = ctx
	_ = tx
	_ = orgID
	_ = monitorID
	_ = latestStatus
	return errors.New("worker.IncidentEngine.Apply: not implemented")
}
