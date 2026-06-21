package memberships

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/authz"
	"github.com/ishaangarg9/statusflow/internal/domain/audit"
	"github.com/ishaangarg9/statusflow/internal/shared"
	"github.com/ishaangarg9/statusflow/internal/tenancy"
)

// Service handles member-management business logic. Every change runs inside
// one tenancy.WithOrgTx (RLS armed) and writes an audit row in that same tx.
// The owner-protection structural guard goes through authz.Can with the
// target's current role in Resource.TargetRole.
type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

type MemberView struct {
	UserID    uuid.UUID  `json:"userId"`
	Email     string     `json:"email"`
	Name      *string    `json:"name,omitempty"`
	Role      authz.Role `json:"role"`
	CreatedAt time.Time  `json:"createdAt"`
}

// List returns every member of the active org. users is a global (non-RLS)
// table; the memberships join is constrained to current_org() by RLS, and we
// also pin org_id explicitly.
func (s *Service) List(ctx context.Context, orgID uuid.UUID) ([]MemberView, error) {
	out := []MemberView{}
	err := tenancy.WithOrgTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT m.user_id, u.email, u.name, m.role, m.created_at
			FROM memberships m
			JOIN users u ON u.id = m.user_id
			WHERE m.org_id = $1
			ORDER BY m.created_at`, orgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v MemberView
			var role string
			if err := rows.Scan(&v.UserID, &v.Email, &v.Name, &role, &v.CreatedAt); err != nil {
				return err
			}
			v.Role = authz.Role(role)
			out = append(out, v)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, shared.Internal(err)
	}
	return out, nil
}

// UpdateRole changes targetUser's role. Authorization is decided here (not the
// handler) because the owner-protection guard needs the target's CURRENT role,
// which requires a read — so the decision and the read share one transaction.
//
// Setting a member to 'owner' is an ownership transfer: it atomically demotes
// the acting owner to 'admin' so the single-owner invariant is preserved.
func (s *Service) UpdateRole(ctx context.Context, ac authz.AuthContext, targetUser uuid.UUID, newRole authz.Role) (*MemberView, error) {
	if !validRole(newRole) {
		return nil, shared.Validation("Invalid role.")
	}
	var v MemberView
	err := tenancy.WithOrgTx(ctx, s.pool, ac.OrgID, func(tx pgx.Tx) error {
		var currentRole string
		err := tx.QueryRow(ctx,
			`SELECT role FROM memberships WHERE org_id = $1 AND user_id = $2`,
			ac.OrgID, targetUser,
		).Scan(&currentRole)
		if err != nil {
			return err // pgx.ErrNoRows -> 404
		}

		// Definitive authz decision now that the target's role is known.
		if !authz.Can(ac, authz.ActionMemberRole, &authz.Resource{
			OrgID: ac.OrgID, TargetRole: authz.Role(currentRole),
		}) {
			return shared.Forbidden()
		}

		transfer := newRole == authz.RoleOwner && authz.Role(currentRole) != authz.RoleOwner
		if transfer {
			// Promote target to owner, demote the acting owner to admin.
			if _, err := tx.Exec(ctx,
				`UPDATE memberships SET role = 'admin' WHERE org_id = $1 AND user_id = $2`,
				ac.OrgID, ac.UserID,
			); err != nil {
				return err
			}
		}

		if err := tx.QueryRow(ctx, `
			UPDATE memberships SET role = $3
			WHERE org_id = $1 AND user_id = $2
			RETURNING user_id, role, created_at`,
			ac.OrgID, targetUser, string(newRole),
		).Scan(&v.UserID, new(string), &v.CreatedAt); err != nil {
			return err
		}
		v.Role = newRole

		// Fill in identity fields for the response.
		if err := tx.QueryRow(ctx,
			`SELECT email, name FROM users WHERE id = $1`, targetUser,
		).Scan(&v.Email, &v.Name); err != nil {
			return err
		}

		return audit.Record(ctx, tx, audit.Entry{
			OrgID:        ac.OrgID,
			ActorUserID:  ac.UserID,
			Action:       "member:role:update",
			ResourceType: "membership",
			ResourceID:   &targetUser,
			Metadata:     map[string]any{"from": currentRole, "to": string(newRole), "ownershipTransfer": transfer},
		})
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.NotFound()
		}
		return nil, mapErr(err)
	}
	return &v, nil
}

// Remove drops targetUser from the org. An owner cannot be removed via this
// endpoint — ownership must be transferred first (otherwise the org could be
// orphaned). The owner-protection guard (admin can't touch an owner) runs
// through authz.Can with the target's current role.
func (s *Service) Remove(ctx context.Context, ac authz.AuthContext, targetUser uuid.UUID) error {
	err := tenancy.WithOrgTx(ctx, s.pool, ac.OrgID, func(tx pgx.Tx) error {
		var currentRole string
		err := tx.QueryRow(ctx,
			`SELECT role FROM memberships WHERE org_id = $1 AND user_id = $2`,
			ac.OrgID, targetUser,
		).Scan(&currentRole)
		if err != nil {
			return err // pgx.ErrNoRows -> 404
		}
		if !authz.Can(ac, authz.ActionMemberRemove, &authz.Resource{
			OrgID: ac.OrgID, TargetRole: authz.Role(currentRole),
		}) {
			return shared.Forbidden()
		}
		if authz.Role(currentRole) == authz.RoleOwner {
			return shared.Validation("Transfer ownership before removing the owner.")
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM memberships WHERE org_id = $1 AND user_id = $2`,
			ac.OrgID, targetUser,
		); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{
			OrgID:        ac.OrgID,
			ActorUserID:  ac.UserID,
			Action:       "member:remove",
			ResourceType: "membership",
			ResourceID:   &targetUser,
			Metadata:     map[string]any{"role": currentRole},
		})
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NotFound()
		}
		return mapErr(err)
	}
	return nil
}

func validRole(r authz.Role) bool {
	switch r {
	case authz.RoleOwner, authz.RoleAdmin, authz.RoleMember, authz.RoleViewer:
		return true
	}
	return false
}

// mapErr passes AppErrors (e.g. Forbidden/Validation raised inside the tx)
// straight through; everything else becomes a 500.
func mapErr(err error) error {
	var ae *shared.AppError
	if errors.As(err, &ae) {
		return ae
	}
	return shared.Internal(err)
}
