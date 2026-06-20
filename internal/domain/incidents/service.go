package incidents

import "github.com/jackc/pgx/v5/pgxpool"

// Service hosts the manual incident API. The worker has its own automatic
// open/resolve state machine in internal/worker/incidents.go that shares
// the same partial unique index (incidents_one_open_per_monitor) so manual
// and automatic flows can't both open an incident simultaneously.
type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }
