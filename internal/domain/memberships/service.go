package memberships

import "github.com/jackc/pgx/v5/pgxpool"

// Service handles member-management business logic.
//
// Implementation outline:
//   - UpdateRole / Remove: gate with authz.Can; target's role goes in Resource.TargetRole
//     so the owner-protection structural guard engages (doc 05 §3.1).
//   - Ownership transfer is atomic inside one WithOrgTx: promote target -> owner,
//     demote self -> admin.
//   - Every change writes an audit_logs row.
type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }
