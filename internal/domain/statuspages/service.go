package statuspages

import "github.com/jackc/pgx/v5/pgxpool"

// Service hosts admin-side and public-side status-page logic.
//
// Public-side rules (doc 04 §8, doc 06 ADR-07):
//   - Resolve org from slug ONLY if is_public = true; otherwise 404.
//   - Open WithOrgTx(org) for a READ-ONLY view; emit the strict projection.
//   - Never select monitored URL, member emails, audit data, or unpublished monitors
//     into the response. Treat this as a hardened, separate code path.
type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }
