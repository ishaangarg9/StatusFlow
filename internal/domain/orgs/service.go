package orgs

import "github.com/jackc/pgx/v5/pgxpool"

// Service implements org-level business logic.
//
// Implementation outline:
//   - Create: NOT inside WithOrgTx (no active org yet). Insert organization,
//     then insert membership(user, org, 'owner') in the same transaction
//     opened on the pool, then SET LOCAL app.current_org_id and write the
//     audit row. The caller of Create is the new owner.
//   - Read/Update/Delete: gate with authz.Can; use tenancy.WithOrgTx; queries
//     include WHERE id = $1 alongside RLS.
//   - Delete: only owner (Can() enforces); audit.
type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }
