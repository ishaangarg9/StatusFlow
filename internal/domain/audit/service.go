package audit

import "github.com/jackc/pgx/v5/pgxpool"

// Service hosts the read API for audit_logs.
//
// Every gated mutation across the codebase writes a row here. The write side
// is colocated with each domain service (so the audit row joins the same
// WithOrgTx as the change). audit:read is itself a gated action (owner/admin).
type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }
