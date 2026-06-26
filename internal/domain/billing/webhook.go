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
	"github.com/ishaangarg9/statusflow/internal/shared"
	"github.com/ishaangarg9/statusflow/internal/stripe"
	"github.com/ishaangarg9/statusflow/internal/tenancy"
)

// ErrInvalidSignature marks a webhook whose body failed signature verification.
// The handler maps it to 400 (forged/garbled — do not act); any other error
// from HandleWebhook is a transient server fault mapped to 500 so Stripe retries.
var ErrInvalidSignature = errors.New("billing: invalid webhook signature")

// knownSubStatuses is the set our subscriptions.status CHECK accepts (mirrors
// Stripe's subscription.status enum). A status outside it — e.g. one Stripe adds
// in a future API version — would violate the CHECK and wedge the webhook in a
// 500/retry loop, so normalizeStatus clamps the unknown to 'incomplete' (which
// degrades to Free — the safe direction) rather than letting the write fail.
var knownSubStatuses = map[string]bool{
	"active": true, "trialing": true, "past_due": true, "canceled": true,
	"incomplete": true, "incomplete_expired": true, "unpaid": true, "paused": true,
}

func normalizeStatus(s string) string {
	if knownSubStatuses[s] {
		return s
	}
	return "incomplete"
}

// planState is the normalized result of interpreting one webhook event: which
// org (may be Nil until the customer-id fallback resolves it), the subscription
// columns to upsert, and the event's timestamp for the ordering guard.
type planState struct {
	orgID      uuid.UUID
	plan       entitlements.Plan
	status     string
	customerID string
	subID      string
	periodEnd  *time.Time
	eventAt    time.Time
}

// HandleWebhook verifies and applies a Stripe webhook. It is the ONLY path that
// mutates plan state, and it trusts nothing until VerifyWebhook passes. After
// that the org is taken from the (now-authenticated) event — its metadata, or a
// fallback lookup by the stored Stripe customer id — and the subscription row is
// upserted inside WithOrgTx(org) with an audit row, exactly like an authenticated
// API write.
//
// Robustness against Stripe's at-least-once, unordered delivery:
//   - ordering/idempotency: the upsert applies only when the event is at least as
//     new as the last one applied (last_stripe_event_at), so a replayed or
//     out-of-order event can't move plan state backwards in time;
//   - unknown status: clamped (normalizeStatus) so it can't violate the CHECK;
//   - customer-id collision: a unique-violation is acked (it's a data conflict a
//     retry can't fix) instead of looping on a 500.
//
// Irrelevant event types and events we still can't attribute to an org are
// acknowledged (nil) so Stripe stops retrying; only signature and transient DB
// failures return errors.
func (s *Service) HandleWebhook(ctx context.Context, payload []byte, sigHeader string) error {
	event, err := stripe.VerifyWebhook(payload, sigHeader, s.cfg.WebhookSecret, time.Now())
	if err != nil {
		return errors.Join(ErrInvalidSignature, err)
	}

	st, handled, err := interpret(event)
	if err != nil {
		s.log.Warn("billing webhook: undecodable", "event", event.Type, "id", event.ID, "err", err)
		return nil // malformed payload for a type we handle — acking avoids a retry storm
	}
	if !handled {
		return nil // event type we don't care about
	}

	// Resolve the org. Metadata/client_reference_id is authoritative; if absent,
	// fall back to the customer id we stored at checkout (SECURITY DEFINER lookup).
	if st.orgID == uuid.Nil {
		org, err := s.resolveOrgByCustomer(ctx, st.customerID)
		if err != nil {
			s.log.Error("billing webhook: org lookup", "event", event.Type, "id", event.ID, "err", err)
			return err // transient DB error → let Stripe retry
		}
		if org == uuid.Nil {
			s.log.Warn("billing webhook: unattributable event", "event", event.Type, "id", event.ID)
			return nil // not ours / unknown customer — ack
		}
		st.orgID = org
	}

	err = tenancy.WithOrgTx(ctx, s.pool, st.orgID, func(tx pgx.Tx) error {
		// Upsert the org's single subscription row. COALESCE keeps a previously
		// stored customer/subscription/period when this event omits it (e.g.
		// checkout.session.completed carries no period end). The DO UPDATE ... WHERE
		// is the ordering guard: skip the update when the incoming event is older
		// than the last applied one (RowsAffected then 0 → a stale/duplicate event,
		// which we treat as a no-op and do not audit).
		cmd, err := tx.Exec(ctx, `
			INSERT INTO subscriptions
			    (org_id, plan, status, stripe_customer_id, stripe_subscription_id,
			     current_period_end, last_stripe_event_at)
			VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6, $7)
			ON CONFLICT (org_id) DO UPDATE SET
			    plan                   = EXCLUDED.plan,
			    status                 = EXCLUDED.status,
			    stripe_customer_id     = COALESCE(EXCLUDED.stripe_customer_id, subscriptions.stripe_customer_id),
			    stripe_subscription_id = COALESCE(EXCLUDED.stripe_subscription_id, subscriptions.stripe_subscription_id),
			    current_period_end     = COALESCE(EXCLUDED.current_period_end, subscriptions.current_period_end),
			    last_stripe_event_at   = EXCLUDED.last_stripe_event_at,
			    updated_at             = now()
			WHERE subscriptions.last_stripe_event_at IS NULL
			   OR EXCLUDED.last_stripe_event_at >= subscriptions.last_stripe_event_at`,
			st.orgID, string(st.plan), st.status, st.customerID, st.subID, st.periodEnd, st.eventAt,
		)
		if err != nil {
			return err
		}
		if cmd.RowsAffected() == 0 {
			return nil // stale/duplicate event — nothing changed, nothing to audit
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
		// A customer-id collision (one customer mapped to two orgs) violates the
		// partial unique index. A retry can't fix a data conflict, so ack it loudly
		// instead of looping on a 500.
		if shared.IsUniqueViolation(err) {
			s.log.Error("billing webhook: customer/org conflict", "event", event.Type, "id", event.ID, "err", err)
			return nil
		}
		s.log.Error("billing webhook: apply", "event", event.Type, "id", event.ID, "err", err)
		return err // 500 → Stripe retries
	}
	return nil
}

// resolveOrgByCustomer looks up the owning org for a Stripe customer id via the
// SECURITY DEFINER subscription_org_by_customer (the sanctioned cross-tenant
// escape hatch — there is no org context here). Returns uuid.Nil when the
// customer is empty or unknown.
func (s *Service) resolveOrgByCustomer(ctx context.Context, customerID string) (uuid.UUID, error) {
	if customerID == "" {
		return uuid.Nil, nil
	}
	var org *uuid.UUID
	if err := s.pool.QueryRow(ctx,
		`SELECT subscription_org_by_customer($1)`, customerID).Scan(&org); err != nil {
		return uuid.Nil, err
	}
	if org == nil {
		return uuid.Nil, nil
	}
	return *org, nil
}

// interpret turns a verified event into a planState. handled=false means "ignore
// this event" (an unhandled type). The org may be Nil here — HandleWebhook
// resolves it (metadata first, customer-id fallback second).
func interpret(e *stripe.Event) (planState, bool, error) {
	eventAt := time.Unix(e.Created, 0).UTC()

	switch e.Type {
	case "checkout.session.completed":
		var obj struct {
			ClientReferenceID string            `json:"client_reference_id"`
			Customer          string            `json:"customer"`
			Subscription      string            `json:"subscription"`
			PaymentStatus     string            `json:"payment_status"`
			Metadata          map[string]string `json:"metadata"`
		}
		if err := json.Unmarshal(e.Object, &obj); err != nil {
			return planState{}, false, err
		}
		// Don't assume the subscription is active just because checkout completed:
		// in async-payment flows the payment may still be pending. Only grant Pro
		// caps once the session is actually paid; otherwise mark it incomplete
		// (degrades to Free) and let the customer.subscription.* events confirm.
		status := "incomplete"
		if obj.PaymentStatus == "paid" || obj.PaymentStatus == "no_payment_required" {
			status = "active"
		}
		return planState{
			orgID:      orgFrom(obj.ClientReferenceID, obj.Metadata),
			plan:       entitlements.PlanPro,
			status:     status,
			customerID: obj.Customer,
			subID:      obj.Subscription,
			eventAt:    eventAt,
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
		st := planState{
			orgID:      orgFrom("", obj.Metadata),
			status:     normalizeStatus(obj.Status),
			customerID: obj.Customer,
			subID:      obj.ID,
			eventAt:    eventAt,
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

// orgFrom resolves the tenant id from the event payload: client_reference_id
// first (checkout), then metadata["org_id"] (subscription events). Returns
// uuid.Nil when neither is a valid uuid; HandleWebhook then tries the customer
// fallback before giving up.
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
