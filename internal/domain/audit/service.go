package audit

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/shared"
	"github.com/ishaangarg9/statusflow/internal/tenancy"
)

// Service hosts the read API for audit_logs. The write side is colocated with
// each domain service via Record (so the audit row joins the same WithOrgTx as
// the change). audit:read is a gated action (owner/admin), enforced in handler.
type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

type EntryView struct {
	Actor        *string    `json:"actor"` // email, or null for a system actor
	Action       string     `json:"action"`
	ResourceType *string    `json:"resourceType,omitempty"`
	ResourceID   *uuid.UUID `json:"resourceId,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
}

// List returns the org's audit entries, newest first. The user join is to the
// global users table; audit_logs rows are constrained to current_org() by RLS.
func (s *Service) List(ctx context.Context, orgID uuid.UUID, limit int) ([]EntryView, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	out := []EntryView{}
	err := tenancy.WithOrgTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT u.email, a.action, a.resource_type, a.resource_id, a.created_at
			FROM audit_logs a
			LEFT JOIN users u ON u.id = a.actor_user_id
			WHERE a.org_id = $1
			ORDER BY a.created_at DESC
			LIMIT $2`, orgID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v EntryView
			if err := rows.Scan(&v.Actor, &v.Action, &v.ResourceType, &v.ResourceID, &v.CreatedAt); err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, shared.Internal(err)
	}
	return out, nil
}
