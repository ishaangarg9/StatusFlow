package incidents

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/authz"
	"github.com/ishaangarg9/statusflow/internal/domain/audit"
	"github.com/ishaangarg9/statusflow/internal/shared"
	"github.com/ishaangarg9/statusflow/internal/tenancy"
)

// Service hosts the manual incident API. The worker has its own automatic
// open/resolve state machine in internal/worker/incidents.go that shares
// the same partial unique index (incidents_one_open_per_monitor) so manual
// and automatic flows can't both open an incident simultaneously.
//
// Every operation runs inside one tenancy.WithOrgTx (RLS armed) and every
// mutation writes an audit row on that same transaction.
type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// IncidentView is the API projection of an incidents row.
type IncidentView struct {
	ID         uuid.UUID  `json:"id"`
	MonitorID  uuid.UUID  `json:"monitorId"`
	Status     string     `json:"status"`
	Title      string     `json:"title"`
	StartedAt  time.Time  `json:"startedAt"`
	ResolvedAt *time.Time `json:"resolvedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	Updates    []Update   `json:"updates,omitempty"`
}

// Update is one incident_updates row.
type Update struct {
	ID        uuid.UUID  `json:"id"`
	Message   string     `json:"message"`
	Status    string     `json:"status"`
	AuthorID  *uuid.UUID `json:"authorId,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

type CreateInput struct {
	MonitorID uuid.UUID
	Title     string
}

type UpdateInput struct {
	Message string
	Status  string
}

// List returns the org's incidents, newest first (no nested updates).
func (s *Service) List(ctx context.Context, orgID uuid.UUID) ([]IncidentView, error) {
	out := []IncidentView{}
	err := tenancy.WithOrgTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, monitor_id, status, title, started_at, resolved_at, created_at
			FROM incidents
			WHERE org_id = $1
			ORDER BY started_at DESC`, orgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v IncidentView
			if err := rows.Scan(&v.ID, &v.MonitorID, &v.Status, &v.Title,
				&v.StartedAt, &v.ResolvedAt, &v.CreatedAt); err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, shared.MapAppErr(err)
	}
	return out, nil
}

// Get returns one incident with its full update timeline.
func (s *Service) Get(ctx context.Context, orgID, id uuid.UUID) (*IncidentView, error) {
	var v IncidentView
	err := tenancy.WithOrgTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			SELECT id, monitor_id, status, title, started_at, resolved_at, created_at
			FROM incidents
			WHERE org_id = $1 AND id = $2`, orgID, id,
		).Scan(&v.ID, &v.MonitorID, &v.Status, &v.Title,
			&v.StartedAt, &v.ResolvedAt, &v.CreatedAt); err != nil {
			return err // ErrNoRows -> 404
		}
		updates, err := loadUpdates(ctx, tx, orgID, id)
		if err != nil {
			return err
		}
		v.Updates = updates
		return nil
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.NotFound()
		}
		return nil, shared.MapAppErr(err)
	}
	return &v, nil
}

func loadUpdates(ctx context.Context, tx pgx.Tx, orgID, incidentID uuid.UUID) ([]Update, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, message, status, author_id, created_at
		FROM incident_updates
		WHERE org_id = $1 AND incident_id = $2
		ORDER BY created_at`, orgID, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	updates := []Update{}
	for rows.Next() {
		var u Update
		if err := rows.Scan(&u.ID, &u.Message, &u.Status, &u.AuthorID, &u.CreatedAt); err != nil {
			return nil, err
		}
		updates = append(updates, u)
	}
	return updates, rows.Err()
}

// Create opens a manual incident for a monitor. The monitor must belong to the
// active org (otherwise it is invisible under RLS → 404). The partial unique
// index incidents_one_open_per_monitor means a second open incident for the
// same monitor is a 409.
func (s *Service) Create(ctx context.Context, ac authz.AuthContext, in CreateInput) (*IncidentView, error) {
	if !authz.Can(ac, authz.ActionIncidentCreate, &authz.Resource{OrgID: ac.OrgID}) {
		return nil, shared.Forbidden()
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, shared.Validation("An incident title is required.")
	}
	if in.MonitorID == uuid.Nil {
		return nil, shared.Validation("A monitorId is required.")
	}

	var v IncidentView
	err := tenancy.WithOrgTx(ctx, s.pool, ac.OrgID, func(tx pgx.Tx) error {
		// Confirm the monitor is in this org so a bad monitorId is a clean 404
		// rather than a foreign-key 500.
		var exists bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM monitors WHERE org_id = $1 AND id = $2)`,
			ac.OrgID, in.MonitorID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return pgx.ErrNoRows
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO incidents (org_id, monitor_id, title, status)
			VALUES ($1, $2, $3, 'open')
			RETURNING id, monitor_id, status, title, started_at, resolved_at, created_at`,
			ac.OrgID, in.MonitorID, title,
		).Scan(&v.ID, &v.MonitorID, &v.Status, &v.Title,
			&v.StartedAt, &v.ResolvedAt, &v.CreatedAt); err != nil {
			if shared.IsUniqueViolation(err) {
				return shared.Conflict("An incident is already open for this monitor.")
			}
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{
			OrgID:        ac.OrgID,
			ActorUserID:  ac.UserID,
			Action:       "incident:create",
			ResourceType: "incident",
			ResourceID:   &v.ID,
			Metadata:     map[string]any{"monitorId": in.MonitorID.String(), "title": title},
		})
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.NotFound()
		}
		return nil, shared.MapAppErr(err)
	}
	return &v, nil
}

// AddUpdate appends a status update to an incident and, when the update's
// status differs, advances the incident's own status (without resolving — that
// is the dedicated Resolve path). Returns the refreshed incident with updates.
func (s *Service) AddUpdate(ctx context.Context, ac authz.AuthContext, incidentID uuid.UUID, in UpdateInput) (*IncidentView, error) {
	if !authz.Can(ac, authz.ActionIncidentUpdate, &authz.Resource{OrgID: ac.OrgID}) {
		return nil, shared.Forbidden()
	}
	message := strings.TrimSpace(in.Message)
	if message == "" {
		return nil, shared.Validation("An update message is required.")
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		return nil, shared.Validation("An update status is required.")
	}

	var v IncidentView
	err := tenancy.WithOrgTx(ctx, s.pool, ac.OrgID, func(tx pgx.Tx) error {
		// The incident must exist in this org. Lock it so a concurrent resolve
		// doesn't race the update insert.
		var current string
		if err := tx.QueryRow(ctx,
			`SELECT status FROM incidents WHERE org_id = $1 AND id = $2 FOR UPDATE`,
			ac.OrgID, incidentID).Scan(&current); err != nil {
			return err // ErrNoRows -> 404
		}
		updateID := uuid.New()
		if _, err := tx.Exec(ctx, `
			INSERT INTO incident_updates (id, org_id, incident_id, message, status, author_id)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			updateID, ac.OrgID, incidentID, message, status, ac.UserID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			SELECT id, monitor_id, status, title, started_at, resolved_at, created_at
			FROM incidents WHERE org_id = $1 AND id = $2`, ac.OrgID, incidentID,
		).Scan(&v.ID, &v.MonitorID, &v.Status, &v.Title,
			&v.StartedAt, &v.ResolvedAt, &v.CreatedAt); err != nil {
			return err
		}
		updates, err := loadUpdates(ctx, tx, ac.OrgID, incidentID)
		if err != nil {
			return err
		}
		v.Updates = updates
		return audit.Record(ctx, tx, audit.Entry{
			OrgID:        ac.OrgID,
			ActorUserID:  ac.UserID,
			Action:       "incident:update",
			ResourceType: "incident",
			ResourceID:   &incidentID,
			Metadata:     map[string]any{"status": status},
		})
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.NotFound()
		}
		return nil, shared.MapAppErr(err)
	}
	return &v, nil
}

// Resolve closes an open incident. Resolving an already-resolved incident is a
// 409; an unknown incident is a 404.
func (s *Service) Resolve(ctx context.Context, ac authz.AuthContext, incidentID uuid.UUID) (*IncidentView, error) {
	if !authz.Can(ac, authz.ActionIncidentResolve, &authz.Resource{OrgID: ac.OrgID}) {
		return nil, shared.Forbidden()
	}
	var v IncidentView
	err := tenancy.WithOrgTx(ctx, s.pool, ac.OrgID, func(tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx,
			`SELECT status FROM incidents WHERE org_id = $1 AND id = $2 FOR UPDATE`,
			ac.OrgID, incidentID).Scan(&status); err != nil {
			return err // ErrNoRows -> 404
		}
		if status == "resolved" {
			return shared.Conflict("This incident is already resolved.")
		}
		if err := tx.QueryRow(ctx, `
			UPDATE incidents SET status = 'resolved', resolved_at = now()
			WHERE org_id = $1 AND id = $2
			RETURNING id, monitor_id, status, title, started_at, resolved_at, created_at`,
			ac.OrgID, incidentID,
		).Scan(&v.ID, &v.MonitorID, &v.Status, &v.Title,
			&v.StartedAt, &v.ResolvedAt, &v.CreatedAt); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{
			OrgID:        ac.OrgID,
			ActorUserID:  ac.UserID,
			Action:       "incident:resolve",
			ResourceType: "incident",
			ResourceID:   &incidentID,
		})
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.NotFound()
		}
		return nil, shared.MapAppErr(err)
	}
	return &v, nil
}
