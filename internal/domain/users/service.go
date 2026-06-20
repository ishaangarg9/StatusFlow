package users

import "github.com/jackc/pgx/v5/pgxpool"

// Service holds the dependencies the users domain needs.
//
// Implementation outline (see CLAUDE.md §5 recipe):
//   - Signup: validate email/password -> HashPassword -> INSERT user (handles unique violation -> 409).
//   - Login: SELECT by email -> VerifyPassword -> SessionStore.Create -> SetCookie.
//   - These touch only global tables (users, sessions), so no WithOrgTx is needed.
type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }
