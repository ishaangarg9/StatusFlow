package billing

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ishaangarg9/statusflow/internal/domain/audit"
	"github.com/ishaangarg9/statusflow/internal/entitlements"
	"github.com/ishaangarg9/statusflow/internal/stripe"
	"github.com/ishaangarg9/statusflow/internal/tenancy"
)

// ErrInvalidSignature marks a webhook whose body failed signature verification.
// The handler maps it to 400 (forged/garbled — do not act); any other error
// from HandleWebhook is a transient server fault mapped to 500 so Stripe retries.
var ErrInvalidSignature = errors.New("billing: invalid webhook signature")

// planState is the normalized result of interpreting one webhook event: which
// org, and the subscription columns to upsert.
type planState struct {
	orgID      uuid.UUID
	plan       entitlements.Plan
	status     string
	customerID string
	subID      string
	periodEnd  *time.Time
}

// HandleWebhook verifies and applies a Stripe webhook. It is the ONLY path that
// mutates plan state, and it trusts nothing until VerifyWebhook passes. After
// that the org is taken from the (now-authenticated) event payload and the
// subscription row is upserted inside WithOrgTx(org) with an audit row — exactly
// like an authenticated API write.
//
// Irrelevant event types and events we can't tie to an org are acknowledged
// (nil) so Stripe stops retrying; only signature and DB failures return errors.
func (s *Service) HandleWebhook(ctx context.Context, payload []byte, sigHeader string) error {
	event, err := stripe.VerifyWebhook(payload, sigHeader, s.cfg.WebhookSecret, time.Now())
	if err != nil {
		return errors.Join(ErrInvalidSignature, err)
	}

	st, ok, err := s.interpret(event)
	if err != nil {
		s.log.Warn("billing webhook: undecodable", "event", event.Type, "id", event.ID, "err", err)
		return nil // malformed payload for a type we handle — acking avoids a retry storm
	}
	if !ok {
		return nil // event type we don't care about, or no resolvable org
	}

	err = tenancy.WithOrgTx(ctx, s.pool, st.orgID, func(tx pgx.Tx) error {
		// Upsert the org's single subscription row. COALESCE keeps a previously
		// stored customer/subscription/period when this particular event omits it
		// (e.g. checkout.session.completed carries no period end).
		if _, err := tx.Exec(ctx, `
			INSERT INTO subscriptions
			    (org_id, plan, status, stripe_customer_id, stripe_subscription_id, current_period_end)
			VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6)
			ON CONFLICT (org_id) DO UPDATE SET
			    plan                   = EXCLUDED.plan,
			    status                 = EXCLUDED.status,
			    stripe_customer_id     = COALESCE(EXCLUDED.stripe_customer_id, subscriptions.stripe_customer_id),
			    stripe_subscription_id = COALESCE(EXCLUDED.stripe_subscription_id, subscriptions.stripe_subscription_id),
			    current_period_end     = COALESCE(EXCLUDED.current_period_end, subscriptions.current_period_end),
			    updated_at             = now()`,
			st.orgID, string(st.plan), st.status, st.customerID, st.subID, st.periodEnd,
		); err != nil {
			return err
		}
		// System-authored audit row (nil actor): the change came from Stripe.
		return audit.Record(ctx, tx, audit.Entry{
			OrgID:        st.orgID,
			Action:       "billing:" + event.Type,
			ResourceType: "subscription",
			Metadata:     map[string]any{"plan": string(st.plan), "status": st.status, "stripeEventId": event.ID},
		})
	})
	if err != nil {
		s.log.Error("billing webhook: apply", "event", event.Type, "id", event.ID, "err", err)
		return err // 500 → Stripe retries
	}
	return nil
}

// interpret turns a verified event into a planState. ok=false means "ignore this
// event" (unhandled type or no org id present).
func (s *Service) interpret(e *stripe.Event) (planState, bool, error) {
	switch e.Type {
	case "checkout.session.completed":
		var obj struct {
			ClientReferenceID string            `json:"client_reference_id"`
			Customer          string            `json:"customer"`
			Subscription      string            `json:"subscription"`
			Metadata          map[string]string `json:"metadata"`
		}
		if err := json.Unmarshal(e.Object, &obj); err != nil {
			return planState{}, false, err
		}
		org := orgFrom(obj.ClientReferenceID, obj.Metadata)
		if org == uuid.Nil {
			return planState{}, false, nil
		}
		return planState{
			orgID:      org,
			plan:       entitlements.PlanPro,
			status:     "active",
			customerID: obj.Customer,
			subID:      obj.Subscription,
		}, true, nil

	case "customer.subscription.created",
		"customer.subscription.updated",
		"customer.subscription.deleted":
		var obj struct {
			ID               string            `json:"id"`
			Customer         string            `json:"customer"`
			Status           string            `json:"status"`
			CurrentPeriodEnd int64             `json:"current_period_end"`
			Metadata         map[string]string `json:"metadata"`
		}
		if err := json.Unmarshal(e.Object, &obj); err != nil {
			return planState{}, false, err
		}
		org := orgFrom("", obj.Metadata)
		if org == uuid.Nil {
			return planState{}, false, nil
		}
		st := planState{
			orgID:      org,
			status:     obj.Status,
			customerID: obj.Customer,
			subID:      obj.ID,
		}
		if e.Type == "customer.subscription.deleted" {
			// A deleted subscription drops the org back to Free regardless of the
			// status Stripe sends with the deletion event.
			st.plan = entitlements.PlanFree
			st.status = "canceled"
		} else {
			st.plan = entitlements.PlanPro
		}
		if obj.CurrentPeriodEnd > 0 {
			t := time.Unix(obj.CurrentPeriodEnd, 0).UTC()
			st.periodEnd = &t
		}
		return st, true, nil

	default:
		return planState{}, false, nil
	}
}

// orgFrom resolves the tenant id from the event: client_reference_id first
// (checkout), then metadata["org_id"] (subscription events). Returns uuid.Nil
// when neither is a valid uuid, so an event we can't attribute is ignored rather
// than misapplied.
func orgFrom(clientRef string, metadata map[string]string) uuid.UUID {
	for _, candidate := range []string{clientRef, metadata["org_id"]} {
		if candidate == "" {
			continue
		}
		if id, err := uuid.Parse(candidate); err == nil {
			return id
		}
	}
	return uuid.Nil
}
