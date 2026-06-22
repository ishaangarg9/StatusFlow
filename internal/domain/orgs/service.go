package orgs

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

// Service implements org-level business logic. All org-scoped DB work goes
// through tenancy.WithOrgTx so RLS is armed; reads/writes also carry an
// explicit WHERE id = $1 (belt and suspenders).
type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

type OrgView struct {
	ID        uuid.UUID  `json:"id"`
	Name      string     `json:"name"`
	Slug      string     `json:"slug"`
	CreatedAt time.Time  `json:"createdAt"`
	Role      authz.Role `json:"role,omitempty"` // caller's role, when known
}

type CreateInput struct {
	Name string
	Slug string
}

type UpdateInput struct {
	Name *string
	Slug *string
}

// Create makes a new org and atomically enrolls the caller as its owner.
//
// RLS subtlety: organizations has FORCE RLS with WITH CHECK (id = current_org()).
// We therefore pre-generate the id in Go and pin it as current_org BEFORE the
// INSERT, so the new row, the owner membership, and the audit entry all satisfy
// their WITH CHECK clauses inside one transaction. Not org-scoped at the HTTP
// layer (no active org yet) — the caller is any authenticated user.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, in CreateInput) (*OrgView, error) {
	name := strings.TrimSpace(in.Name)
	slug := strings.ToLower(strings.TrimSpace(in.Slug))
	if err := validateName(name); err != nil {
		return nil, err
	}
	if err := validateSlug(slug); err != nil {
		return nil, err
	}

	orgID := uuid.New()
	out := &OrgView{ID: orgID, Name: name, Slug: slug, Role: authz.RoleOwner}

	err := tenancy.WithOrgTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO organizations (id, name, slug)
			VALUES ($1, $2, $3)
			RETURNING created_at`,
			orgID, name, slug,
		).Scan(&out.CreatedAt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO memberships (org_id, user_id, role)
			VALUES ($1, $2, 'owner')`,
			orgID, userID,
		); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{
			OrgID:        orgID,
			ActorUserID:  userID,
			Action:       "org:create",
			ResourceType: "organization",
			ResourceID:   &orgID,
			Metadata:     map[string]any{"name": name, "slug": slug},
		})
	})
	if err != nil {
		if shared.IsUniqueViolation(err) {
			return nil, shared.Conflict("That slug is already taken.")
		}
		return nil, shared.Internal(err)
	}
	return out, nil
}

// Read returns the active org. Role is filled in by the caller from AuthContext.
func (s *Service) Read(ctx context.Context, orgID uuid.UUID) (*OrgView, error) {
	var v OrgView
	err := tenancy.WithOrgTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT id, name, slug, created_at
			FROM organizations WHERE id = $1`, orgID,
		).Scan(&v.ID, &v.Name, &v.Slug, &v.CreatedAt)
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.NotFound()
		}
		return nil, shared.Internal(err)
	}
	return &v, nil
}

// Update renames / re-slugs the org. At least one field must be present.
func (s *Service) Update(ctx context.Context, actorID, orgID uuid.UUID, in UpdateInput) (*OrgView, error) {
	if in.Name == nil && in.Slug == nil {
		return nil, shared.Validation("Nothing to update.")
	}
	var name, slug *string
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if err := validateName(n); err != nil {
			return nil, err
		}
		name = &n
	}
	if in.Slug != nil {
		sl := strings.ToLower(strings.TrimSpace(*in.Slug))
		if err := validateSlug(sl); err != nil {
			return nil, err
		}
		slug = &sl
	}

	var v OrgView
	err := tenancy.WithOrgTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		// Capture the prior values in the same statement so the audit row can
		// record from->to. COALESCE keeps the existing value when a field is
		// omitted.
		var oldName, oldSlug string
		if err := tx.QueryRow(ctx, `
			WITH prev AS (
				SELECT name AS old_name, slug AS old_slug FROM organizations WHERE id = $1
			)
			UPDATE organizations o
			SET name = COALESCE($2, o.name),
			    slug = COALESCE($3, o.slug),
			    updated_at = now()
			FROM prev
			WHERE o.id = $1
			RETURNING o.id, o.name, o.slug, o.created_at, prev.old_name, prev.old_slug`,
			orgID, name, slug,
		).Scan(&v.ID, &v.Name, &v.Slug, &v.CreatedAt, &oldName, &oldSlug); err != nil {
			return err
		}
		meta := map[string]any{}
		if v.Name != oldName {
			meta["name"] = map[string]any{"from": oldName, "to": v.Name}
		}
		if v.Slug != oldSlug {
			meta["slug"] = map[string]any{"from": oldSlug, "to": v.Slug}
		}
		return audit.Record(ctx, tx, audit.Entry{
			OrgID:        orgID,
			ActorUserID:  actorID,
			Action:       "org:update",
			ResourceType: "organization",
			ResourceID:   &orgID,
			Metadata:     meta,
		})
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.NotFound()
		}
		if shared.IsUniqueViolation(err) {
			return nil, shared.Conflict("That slug is already taken.")
		}
		return nil, shared.Internal(err)
	}
	return &v, nil
}

// Delete removes the org and (via ON DELETE CASCADE) all of its tenant data,
// including its per-org audit_logs. The deletion itself is recorded in the
// GLOBAL audit sink (global_audit_logs, no org FK / no RLS) in the same
// transaction, so the destructive offboarding leaves a durable trace even
// though the org's own trail is cascaded away.
func (s *Service) Delete(ctx context.Context, actorID, orgID uuid.UUID) error {
	err := tenancy.WithOrgTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		cmd, err := tx.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, orgID)
		if err != nil {
			return err
		}
		if cmd.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return audit.RecordGlobal(ctx, tx, audit.Entry{
			OrgID:       orgID,
			ActorUserID: actorID,
			Action:      "org:delete",
		})
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NotFound()
		}
		return shared.Internal(err)
	}
	return nil
}

// --- Validation ----------------------------------------------------------

func validateName(s string) error {
	if s == "" || len(s) > 200 {
		return shared.Validation("Name must be 1–200 characters.")
	}
	return nil
}

func validateSlug(s string) error {
	if !shared.ValidSlug(s) {
		return shared.Validation("Slug must be 2–63 chars: lowercase letters, digits, single hyphens.")
	}
	return nil
}
