package invitations

import "github.com/jackc/pgx/v5/pgxpool"

// Service handles invitation issuance and acceptance.
//
// Implementation outline:
//   - Create: gate with Can(member:invite); CSPRNG token via auth.NewSessionToken;
//     store auth.HashToken(token); email the RAW link.
//   - Accept (GLOBAL endpoint — invitee isn't a member yet): look up by token hash;
//     verify not expired/accepted; resolve org_id from the invitation; open
//     tenancy.WithOrgTx(invitation.org_id); INSERT membership; UPDATE invitation
//     accepted_at. The (org_id, user_id) UNIQUE handles double-accept as 409 idempotent.
type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }
