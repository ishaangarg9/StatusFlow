// Package entitlements maps a billing plan to the product limits the server
// enforces. These caps are the teeth behind the pricing page: they are checked
// in the service layer (inside the org's WithOrgTx), never on the client
// (CLAUDE.md invariant 12). A plan upgrade is recorded only via the Stripe
// webhook, so a user cannot grant themselves a higher plan by calling the API.
//
// This package deliberately knows nothing about Stripe — it reads the local
// `subscriptions` projection (written by the webhook) and turns the plan name
// into numbers. Keeping the limits here means monitors / status-pages services
// don't import the billing domain, and the limit table has one home.
package entitlements

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Plan is the billed tier. The set mirrors the subscriptions.plan CHECK.
type Plan string

const (
	PlanFree Plan = "free"
	PlanPro  Plan = "pro"
)

// Unlimited is the sentinel for "no cap" (used by paid plans). A limit of
// Unlimited is never hit; callers compare with WithinLimit.
const Unlimited = -1

// Limits is the set of server-enforced caps for a plan. Add a field here and
// the enforcing service picks it up; default-deny is not a concern because a
// missing/unknown plan resolves to Free (the most restrictive).
type Limits struct {
	MaxMonitors    int // max monitors per org; Unlimited = no cap
	MaxStatusPages int // max status pages per org; Unlimited = no cap
}

// LimitsFor returns the caps for a plan. An unknown plan is treated as Free —
// the safe default, so a bad/garbled plan value can never widen entitlements.
func LimitsFor(plan Plan) Limits {
	switch plan {
	case PlanPro:
		return Limits{MaxMonitors: Unlimited, MaxStatusPages: Unlimited}
	default:
		return Limits{MaxMonitors: 3, MaxStatusPages: 1}
	}
}

// WithinLimit reports whether a resource at the given current count may grow by
// one without exceeding limit. Unlimited always passes.
func WithinLimit(current, limit int) bool {
	return limit == Unlimited || current < limit
}

// PlanForOrg reads the active plan for an org from the local subscriptions
// projection, inside an existing org-scoped transaction (RLS armed). The org is
// Pro only when it has a row whose plan is 'pro' AND whose status still entitles
// it (active or trialing) — a canceled/past_due/unpaid subscription falls back
// to Free so a lapsed payment immediately loses the paid caps. No row => Free.
func PlanForOrg(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (Plan, error) {
	var plan, status string
	err := tx.QueryRow(ctx,
		`SELECT plan, status FROM subscriptions WHERE org_id = $1`, orgID,
	).Scan(&plan, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PlanFree, nil
		}
		return PlanFree, err
	}
	if Plan(plan) == PlanPro && (status == "active" || status == "trialing") {
		return PlanPro, nil
	}
	return PlanFree, nil
}
