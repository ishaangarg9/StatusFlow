package orgs

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
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
		// COALESCE keeps the existing value when a field is omitted.
		if err := tx.QueryRow(ctx, `
			UPDATE organizations
			SET name = COALESCE($2, name),
			    slug = COALESCE($3, slug),
			    updated_at = now()
			WHERE id = $1
			RETURNING id, name, slug, created_at`,
			orgID, name, slug,
		).Scan(&v.ID, &v.Name, &v.Slug, &v.CreatedAt); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{
			OrgID:        orgID,
			ActorUserID:  actorID,
			Action:       "org:update",
			ResourceType: "organization",
			ResourceID:   &orgID,
		})
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.NotFound()
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, shared.Conflict("That slug is already taken.")
		}
		return nil, shared.Internal(err)
	}
	return &v, nil
}

// Delete removes the org and (via ON DELETE CASCADE) all of its tenant data,
// including its audit_logs. We therefore do NOT write an audit row for the
// delete — it would be cascaded away in the same transaction. The org's entire
// trail vanishes with it by design (tenant offboarding).
func (s *Service) Delete(ctx context.Context, orgID uuid.UUID) error {
	err := tenancy.WithOrgTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		cmd, err := tx.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, orgID)
		if err != nil {
			return err
		}
		if cmd.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return nil
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

var slugRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func validateName(s string) error {
	if s == "" || len(s) > 200 {
		return shared.Validation("Name must be 1–200 characters.")
	}
	return nil
}

func validateSlug(s string) error {
	if len(s) < 2 || len(s) > 63 || !slugRe.MatchString(s) {
		return shared.Validation("Slug must be 2–63 chars: lowercase letters, digits, single hyphens.")
	}
	return nil
}
