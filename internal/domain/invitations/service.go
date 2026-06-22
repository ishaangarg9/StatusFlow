package invitations

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/auth"
	"github.com/ishaangarg9/statusflow/internal/authz"
	"github.com/ishaangarg9/statusflow/internal/domain/audit"
	"github.com/ishaangarg9/statusflow/internal/shared"
	"github.com/ishaangarg9/statusflow/internal/tenancy"
)

// inviteTTL is how long an issued invitation stays acceptable.
const inviteTTL = 7 * 24 * time.Hour

// Service handles invitation issuance, listing, revocation, and acceptance.
//
// Issuance/listing/revocation are org-scoped (gated on member:invite, run inside
// tenancy.WithOrgTx with RLS armed). Acceptance is the one pre-membership path:
// the invitee is signed in but not yet a member, so the org is discovered via
// the SECURITY DEFINER invitation_org_by_token() escape hatch and the mutation
// then runs org-scoped.
type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// InvitationView is the non-secret projection. The raw token is NEVER part of
// this — it is delivered out-of-band by the Mailer and the DB stores only its
// hash.
type InvitationView struct {
	ID        uuid.UUID  `json:"id"`
	Email     string     `json:"email"`
	Role      authz.Role `json:"role"`
	ExpiresAt time.Time  `json:"expiresAt"`
	CreatedAt time.Time  `json:"createdAt"`
}

// MembershipResult is what an accepted invite produces.
type MembershipResult struct {
	OrgID uuid.UUID  `json:"orgId"`
	Role  authz.Role `json:"role"`
}

type CreateInput struct {
	Email string
	Role  authz.Role
}

// Create issues (or re-issues) an invitation for email at role and ENQUEUES it
// for delivery; the worker (invitations.Deliverer) mints the token and sends the
// mail asynchronously. Nothing secret is produced here — the invitation is
// stored with token_hash = NULL until the worker delivers it — so there is no
// token to return or log (CLAUDE.md §6). Because delivery is out of band, a mail
// failure can no longer fail this request after the row has committed.
//
// Authorization (member:invite → owner/admin) is enforced here. The service
// additionally refuses to invite at the owner role (ownership is a transfer-only
// flow that must keep the single-owner invariant intact).
//
// On conflict with an existing pending invite for the same (org, email), the
// invite is re-issued (token cleared, fresh expiry) and its delivery re-queued —
// a resend. An email that already belongs to a member is rejected as 409.
func (s *Service) Create(ctx context.Context, ac authz.AuthContext, in CreateInput) (*InvitationView, error) {
	if !authz.Can(ac, authz.ActionMemberInvite, &authz.Resource{OrgID: ac.OrgID}) {
		return nil, shared.Forbidden()
	}

	email := strings.ToLower(strings.TrimSpace(in.Email))
	if !shared.ValidEmail(email) {
		return nil, shared.Validation("A valid email is required.")
	}
	role := in.Role
	if role == "" {
		role = authz.RoleMember
	}
	if !authz.AssignableViaInvite(role) {
		return nil, shared.Validation("Role must be one of: admin, member, viewer.")
	}

	expiresAt := time.Now().Add(inviteTTL)

	var v InvitationView
	err := tenancy.WithOrgTx(ctx, s.pool, ac.OrgID, func(tx pgx.Tx) error {
		// Reject inviting someone who is already a member of this org. The join
		// to users is constrained to current_org() by RLS on memberships.
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM memberships m
				JOIN users u ON u.id = m.user_id
				WHERE m.org_id = $1 AND u.email = $2
			)`, ac.OrgID, email).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return shared.Conflict("That person is already a member of this organization.")
		}

		// Upsert: a fresh invite for a new email, or a resend (token cleared, reset
		// expiry/acceptance) for an existing pending one. token_hash is set to
		// NULL — the worker mints it at delivery. created_at is left untouched on
		// resend so the first-invited time is preserved.
		var roleStr string
		if err := tx.QueryRow(ctx, `
			INSERT INTO invitations (org_id, email, role, token_hash, invited_by, expires_at)
			VALUES ($1, $2, $3, NULL, $4, $5)
			ON CONFLICT (org_id, email) DO UPDATE
			SET role = EXCLUDED.role,
			    token_hash = NULL,
			    invited_by = EXCLUDED.invited_by,
			    expires_at = EXCLUDED.expires_at,
			    accepted_at = NULL
			RETURNING id, email, role, expires_at, created_at`,
			ac.OrgID, email, string(role), ac.UserID, expiresAt,
		).Scan(&v.ID, &v.Email, &roleStr, &v.ExpiresAt, &v.CreatedAt); err != nil {
			return err
		}
		v.Role = authz.Role(roleStr)

		// Enqueue delivery in the SAME tx, so an issued invite is always queued
		// (no committed-but-unqueued window). A resend re-arms the existing row:
		// reset attempts and make it due now.
		if _, err := tx.Exec(ctx, `
			INSERT INTO invitation_outbox (org_id, invitation_id)
			VALUES ($1, $2)
			ON CONFLICT (invitation_id) DO UPDATE
			SET attempts = 0, next_attempt_at = now(), last_error = NULL`,
			ac.OrgID, v.ID); err != nil {
			return err
		}

		return audit.Record(ctx, tx, audit.Entry{
			OrgID:        ac.OrgID,
			ActorUserID:  ac.UserID,
			Action:       "member:invite",
			ResourceType: "invitation",
			ResourceID:   &v.ID,
			Metadata:     map[string]any{"email": email, "role": string(role)},
		})
	})
	if err != nil {
		return nil, shared.MapAppErr(err)
	}
	return &v, nil
}

// List returns the org's live (unaccepted, unexpired) invitations.
func (s *Service) List(ctx context.Context, ac authz.AuthContext) ([]InvitationView, error) {
	if !authz.Can(ac, authz.ActionMemberInvite, &authz.Resource{OrgID: ac.OrgID}) {
		return nil, shared.Forbidden()
	}
	out := []InvitationView{}
	err := tenancy.WithOrgTx(ctx, s.pool, ac.OrgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, email, role, expires_at, created_at
			FROM invitations
			WHERE org_id = $1 AND accepted_at IS NULL AND expires_at > now()
			ORDER BY created_at DESC`, ac.OrgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v InvitationView
			var roleStr string
			if err := rows.Scan(&v.ID, &v.Email, &roleStr, &v.ExpiresAt, &v.CreatedAt); err != nil {
				return err
			}
			v.Role = authz.Role(roleStr)
			out = append(out, v)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, shared.MapAppErr(err)
	}
	return out, nil
}

// Revoke deletes a PENDING invitation by id. Only an unaccepted invite may be
// revoked, so an accepted invitation's historical row is never destroyed. A
// missing/unknown/already-accepted id (including one belonging to another org,
// hidden by RLS) is a 404.
func (s *Service) Revoke(ctx context.Context, ac authz.AuthContext, id uuid.UUID) error {
	if !authz.Can(ac, authz.ActionMemberInvite, &authz.Resource{OrgID: ac.OrgID}) {
		return shared.Forbidden()
	}
	err := tenancy.WithOrgTx(ctx, s.pool, ac.OrgID, func(tx pgx.Tx) error {
		cmd, err := tx.Exec(ctx,
			`DELETE FROM invitations WHERE org_id = $1 AND id = $2 AND accepted_at IS NULL`,
			ac.OrgID, id)
		if err != nil {
			return err
		}
		if cmd.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return audit.Record(ctx, tx, audit.Entry{
			OrgID:        ac.OrgID,
			ActorUserID:  ac.UserID,
			Action:       "member:invite:revoke",
			ResourceType: "invitation",
			ResourceID:   &id,
		})
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NotFound()
		}
		return shared.MapAppErr(err)
	}
	return nil
}

// Accept consumes a raw invite token on behalf of the signed-in user and joins
// them to the org. This is the only pre-membership write: the org is discovered
// via the locked-down invitation_org_by_token() function, then all validation
// and mutation happen inside tenancy.WithOrgTx with RLS armed.
//
// The invite is bound to the email it was issued to: the accepting user's email
// must match, so a leaked token cannot be redeemed by a different account.
// Status codes: invalid/expired token → 422; email mismatch → 403; a token that
// has already been used, or an account that is already a member → 409.
func (s *Service) Accept(ctx context.Context, user *auth.User, rawToken string) (*MembershipResult, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, shared.Validation("A token is required.")
	}
	tokenHash := auth.HashToken(rawToken)

	// Escape hatch: resolve which org this token belongs to. The function is a
	// scalar that returns SQL NULL for an unknown token; scanning into a pointer
	// makes that explicit (nil → invalid token) rather than relying on a
	// zero-value coercion, which matters because this is the one RLS-bypassing
	// read in the system.
	var orgPtr *uuid.UUID
	if err := s.pool.QueryRow(ctx,
		`SELECT invitation_org_by_token($1)`, tokenHash).Scan(&orgPtr); err != nil {
		return nil, shared.Internal(err)
	}
	if orgPtr == nil {
		return nil, shared.Validation("This invitation is invalid or has expired.")
	}
	orgID := *orgPtr

	result := &MembershipResult{OrgID: orgID}
	err := tenancy.WithOrgTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		// Authoritative, race-free re-read under RLS. FOR UPDATE serializes
		// concurrent accepts of the same invite.
		var (
			invID      uuid.UUID
			email      string
			roleStr    string
			expiresAt  time.Time
			acceptedAt *time.Time
		)
		err := tx.QueryRow(ctx, `
			SELECT id, email, role, expires_at, accepted_at
			FROM invitations
			WHERE org_id = $1 AND token_hash = $2
			FOR UPDATE`, orgID, tokenHash,
		).Scan(&invID, &email, &roleStr, &expiresAt, &acceptedAt)
		if err != nil {
			return err // ErrNoRows -> 422 via mapping below
		}
		role := authz.Role(roleStr)

		if acceptedAt != nil {
			return shared.Conflict("This invitation has already been used.")
		}
		if time.Now().After(expiresAt) {
			return shared.Validation("This invitation is invalid or has expired.")
		}
		// Bind the invite to its target email (citext/case-insensitive compare).
		if !strings.EqualFold(strings.TrimSpace(user.Email), email) {
			return shared.Forbidden()
		}

		// Join. The (org_id, user_id) UNIQUE turns a double-join into a 409.
		if _, err := tx.Exec(ctx, `
			INSERT INTO memberships (org_id, user_id, role)
			VALUES ($1, $2, $3)`,
			orgID, user.ID, string(role),
		); err != nil {
			if shared.IsUniqueViolation(err) {
				return shared.Conflict("You are already a member of this organization.")
			}
			return err
		}
		result.Role = role

		if _, err := tx.Exec(ctx,
			`UPDATE invitations SET accepted_at = now() WHERE org_id = $1 AND token_hash = $2`,
			orgID, tokenHash); err != nil {
			return err
		}

		// Drop any pending delivery for this invite so the worker never re-mints a
		// token for an org the user has now joined. Normally already gone (delivery
		// deletes it on send); this covers the deleteOutbox-failed edge.
		if _, err := tx.Exec(ctx,
			`DELETE FROM invitation_outbox WHERE org_id = $1 AND invitation_id = $2`,
			orgID, invID); err != nil {
			return err
		}

		return audit.Record(ctx, tx, audit.Entry{
			OrgID:        orgID,
			ActorUserID:  user.ID,
			Action:       "invitation:accept",
			ResourceType: "membership",
			ResourceID:   &user.ID,
			Metadata:     map[string]any{"role": string(role), "email": email},
		})
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.Validation("This invitation is invalid or has expired.")
		}
		return nil, shared.MapAppErr(err)
	}
	return result, nil
}
