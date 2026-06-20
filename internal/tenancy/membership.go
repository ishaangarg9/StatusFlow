package tenancy

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/authz"
)

type Membership struct {
	OrgID  uuid.UUID
	UserID uuid.UUID
	Role   authz.Role
}

// ResolveMembership returns the (userID, orgID) membership if one exists.
// The query runs inside WithOrgTx, so RLS constrains the memberships table
// to org_id = current_org(). A returned row both confirms membership AND
// reveals the role; no row -> caller turns into 404 (hides org existence).
func ResolveMembership(ctx context.Context, pool *pgxpool.Pool, userID, orgID uuid.UUID) (*Membership, error) {
	var m *Membership
	err := WithOrgTx(ctx, pool, orgID, func(tx pgx.Tx) error {
		var role string
		err := tx.QueryRow(ctx,
			`SELECT role FROM memberships WHERE user_id = $1 AND org_id = $2`,
			userID, orgID,
		).Scan(&role)
		if err != nil {
			return err
		}
		m = &Membership{OrgID: orgID, UserID: userID, Role: authz.Role(role)}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil // caller maps to 404
	}
	return m, err
}
