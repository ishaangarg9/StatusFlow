// Package billing is the monetization domain: it reads the local subscription
// projection, starts Stripe Checkout / Billing Portal flows, and applies the
// signature-verified webhook that is the ONLY writer of plan state.
//
// Invariant boundaries this domain respects:
//   - Stripe is the source of truth for money; we never let the client tell us a
//     plan. StartCheckout/StartPortal only hand back a redirect URL — the plan in
//     `subscriptions` changes solely in HandleWebhook, after signature checks.
//   - subscriptions is a tenant table: every DB write runs inside WithOrgTx with
//     RLS armed, carries WHERE org_id, and lands an audit row on the same tx.
//   - The webhook resolves the org from the (verified) event itself
//     (client_reference_id / subscription metadata), so it needs no SECURITY
//     DEFINER escape hatch — it WithOrgTx(org) like any authenticated write.
package billing

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/authz"
	"github.com/ishaangarg9/statusflow/internal/domain/audit"
	"github.com/ishaangarg9/statusflow/internal/entitlements"
	"github.com/ishaangarg9/statusflow/internal/shared"
	"github.com/ishaangarg9/statusflow/internal/stripe"
	"github.com/ishaangarg9/statusflow/internal/tenancy"
)

// Config carries the Stripe wiring. When Client is nil (no secret key) the app
// runs "monetization-ready but inert": reads still work (every org is Free) and
// the checkout/portal actions return a clear 422 instead of panicking.
type Config struct {
	PriceID       string
	WebhookSecret string
	AppBaseURL    string
}

type Service struct {
	pool   *pgxpool.Pool
	stripe *stripe.Client // nil when Stripe is not configured
	cfg    Config
	log    *slog.Logger
}

func NewService(pool *pgxpool.Pool, client *stripe.Client, cfg Config, log *slog.Logger) *Service {
	return &Service{pool: pool, stripe: client, cfg: cfg, log: log}
}

// SubscriptionView is the billing screen's projection: the plan the org signed
// up for, its Stripe status, the enforced caps, and current usage against them.
type SubscriptionView struct {
	Plan             string     `json:"plan"`
	Status           string     `json:"status"`
	CurrentPeriodEnd *time.Time `json:"currentPeriodEnd,omitempty"`
	Limits           Limits     `json:"limits"`
	Usage            Usage      `json:"usage"`
	// Configured: Stripe keys present so checkout works. HasCustomer: a Stripe
	// customer exists so the Billing Portal can be opened.
	Configured  bool `json:"configured"`
	HasCustomer bool `json:"hasCustomer"`
}

type Limits struct {
	MaxMonitors    int `json:"maxMonitors"`    // -1 = unlimited
	MaxStatusPages int `json:"maxStatusPages"` // -1 = unlimited
}

type Usage struct {
	Monitors    int `json:"monitors"`
	StatusPages int `json:"statusPages"`
}

// Get returns the subscription view for the org's billing screen. Gated by
// billing:read (owner/admin). Limits reflect the EFFECTIVE plan (a past_due Pro
// is enforced as Free), so what the user sees matches what the API enforces.
func (s *Service) Get(ctx context.Context, ac authz.AuthContext) (*SubscriptionView, error) {
	if !authz.Can(ac, authz.ActionBillingRead, &authz.Resource{OrgID: ac.OrgID}) {
		return nil, shared.Forbidden()
	}
	v := &SubscriptionView{
		Plan:       string(entitlements.PlanFree),
		Status:     "active",
		Configured: s.stripe != nil && s.cfg.PriceID != "",
	}
	err := tenancy.WithOrgTx(ctx, s.pool, ac.OrgID, func(tx pgx.Tx) error {
		var (
			plan, status string
			periodEnd    *time.Time
			customerID   *string
		)
		err := tx.QueryRow(ctx,
			`SELECT plan, status, current_period_end, stripe_customer_id
			   FROM subscriptions WHERE org_id = $1`, ac.OrgID,
		).Scan(&plan, &status, &periodEnd, &customerID)
		switch {
		case err == nil:
			v.Plan, v.Status, v.CurrentPeriodEnd = plan, status, periodEnd
			v.HasCustomer = customerID != nil && *customerID != ""
		case errors.Is(err, pgx.ErrNoRows):
			// No row: org is Free with the defaults already set above.
		default:
			return err
		}

		// Effective plan from the row already in hand (no second query) — the same
		// rule the API enforces, so the screen and the gate never disagree.
		effective := entitlements.EffectivePlan(entitlements.Plan(v.Plan), v.Status)
		lim := entitlements.LimitsFor(effective)
		v.Limits = Limits{MaxMonitors: lim.MaxMonitors, MaxStatusPages: lim.MaxStatusPages}

		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM monitors WHERE org_id = $1`, ac.OrgID,
		).Scan(&v.Usage.Monitors); err != nil {
			return err
		}
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM status_pages WHERE org_id = $1`, ac.OrgID,
		).Scan(&v.Usage.StatusPages)
	})
	if err != nil {
		return nil, shared.MapAppErr(err)
	}
	return v, nil
}

// StartCheckout creates a Stripe Checkout Session for the Pro plan and returns
// the hosted-checkout URL. Gated by billing:manage (owner only). It reuses the
// org's existing Stripe customer when one exists (so re-subscribing doesn't mint
// a duplicate) and pre-fills the owner's email otherwise.
func (s *Service) StartCheckout(ctx context.Context, ac authz.AuthContext) (string, error) {
	if !authz.Can(ac, authz.ActionBillingManage, &authz.Resource{OrgID: ac.OrgID}) {
		return "", shared.Forbidden()
	}
	if s.stripe == nil || s.cfg.PriceID == "" {
		return "", shared.Validation("Billing is not configured on this deployment.")
	}

	// One tx: read any existing customer (RLS-scoped) and audit the intent
	// together, BEFORE the external Stripe call. Auditing the attempt up front
	// (rather than after a successful session) keeps the audit atomic with a
	// single transaction and records the action even if Stripe then errors.
	var customerID string
	if err := tenancy.WithOrgTx(ctx, s.pool, ac.OrgID, func(tx pgx.Tx) error {
		c, err := customerIDFromTx(ctx, tx, ac.OrgID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		customerID = c
		return audit.Record(ctx, tx, audit.Entry{
			OrgID:        ac.OrgID,
			ActorUserID:  ac.UserID,
			Action:       "billing:checkout_start",
			ResourceType: "subscription",
		})
	}); err != nil {
		return "", shared.MapAppErr(err)
	}

	var email string
	// users is global (no RLS) — read on the bare pool.
	_ = s.pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, ac.UserID).Scan(&email)

	billingURL := s.cfg.AppBaseURL + "/orgs/" + ac.OrgID.String() + "/billing"
	url, err := s.stripe.CreateCheckoutSession(ctx, stripe.CheckoutParams{
		PriceID:    s.cfg.PriceID,
		SuccessURL: billingURL + "?checkout=success",
		CancelURL:  billingURL + "?checkout=cancelled",
		OrgID:      ac.OrgID.String(),
		CustomerID: customerID,
		Email:      email,
	})
	if err != nil {
		s.log.Error("stripe checkout", "err", err, "org_id", ac.OrgID)
		return "", shared.Internal(err)
	}
	return url, nil
}

// customerIDFromTx reads the org's stored Stripe customer id within an existing
// org-scoped tx. Returns ("", pgx.ErrNoRows) when the org has no subscription row
// yet, and ("", nil) when the row exists but the customer is unset — the two
// callers (checkout/portal) distinguish those cases themselves.
func customerIDFromTx(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) (string, error) {
	var c *string
	if err := tx.QueryRow(ctx,
		`SELECT stripe_customer_id FROM subscriptions WHERE org_id = $1`, orgID).Scan(&c); err != nil {
		return "", err
	}
	if c == nil {
		return "", nil
	}
	return *c, nil
}

// StartPortal opens a Stripe Billing Portal session for the org's customer and
// returns the URL. Gated by billing:manage (owner only). An org that never
// subscribed has no customer, so there is nothing to manage → 422.
func (s *Service) StartPortal(ctx context.Context, ac authz.AuthContext) (string, error) {
	if !authz.Can(ac, authz.ActionBillingManage, &authz.Resource{OrgID: ac.OrgID}) {
		return "", shared.Forbidden()
	}
	if s.stripe == nil {
		return "", shared.Validation("Billing is not configured on this deployment.")
	}

	// One tx: confirm the org has a Stripe customer and audit the intent together.
	// No customer (no subscription row, or the row predates checkout) → 422; the
	// shared.Validation returned from the closure rolls the tx back (no audit).
	var customerID string
	if err := tenancy.WithOrgTx(ctx, s.pool, ac.OrgID, func(tx pgx.Tx) error {
		c, err := customerIDFromTx(ctx, tx, ac.OrgID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && c == "") {
			return shared.Validation("No billing account yet. Subscribe first.")
		}
		if err != nil {
			return err
		}
		customerID = c
		return audit.Record(ctx, tx, audit.Entry{
			OrgID:        ac.OrgID,
			ActorUserID:  ac.UserID,
			Action:       "billing:portal_open",
			ResourceType: "subscription",
		})
	}); err != nil {
		return "", shared.MapAppErr(err)
	}

	url, err := s.stripe.CreatePortalSession(ctx, customerID,
		s.cfg.AppBaseURL+"/orgs/"+ac.OrgID.String()+"/billing")
	if err != nil {
		s.log.Error("stripe portal", "err", err, "org_id", ac.OrgID)
		return "", shared.Internal(err)
	}
	return url, nil
}
