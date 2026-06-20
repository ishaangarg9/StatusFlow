package tenancy

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// WithOrgTx opens a transaction, pins the active org as a TRANSACTION-LOCAL
// setting, then runs fn with that tx. RLS reads app.current_org_id from this
// connection. Because set_config(..., true) is transaction-local, the value
// cannot leak across pooled connections.
//
// This is the isolation linchpin. Never run an org-scoped query on the bare pool.
func WithOrgTx(ctx context.Context, pool *pgxpool.Pool, orgID uuid.UUID, fn func(tx pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // no-op after a successful Commit

	if _, err := tx.Exec(ctx,
		"SELECT set_config('app.current_org_id', $1, true)", orgID.String(),
	); err != nil {
		return err
	}

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
