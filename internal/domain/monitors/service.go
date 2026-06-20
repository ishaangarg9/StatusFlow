package monitors

import "github.com/jackc/pgx/v5/pgxpool"

// Service is the monitors business layer.
//
// Implementation outline (CLAUDE.md §5 recipe applied):
//   1. Gate with authz.Can.
//   2. tenancy.WithOrgTx(ctx, pool, ac.OrgID, func(tx pgx.Tx) error { ... })
//   3. Inside the tx: use sqlc.New(tx) so every query runs on the org-pinned tx;
//      RLS engages on app.current_org_id = ac.OrgID.
//   4. Each query still carries WHERE org_id = $1 — belt and suspenders.
//   5. On mutation, insert audit_logs row inside the same tx.
type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }
