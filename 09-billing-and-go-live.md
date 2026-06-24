# 09 — Billing & Go-Live (Phase 10)

StatusFlow is **monetization-ready, not monetized**: Stripe runs in **test mode**,
the plans and entitlements are real and server-enforced, but no real customers,
real charges, or legal surface exist yet. This doc is the design of the billing
layer and the checklist to flip it to live.

---

## 1. What billing adds

| Piece | Where |
|-------|-------|
| `subscriptions` tenant table (one row per org) | migration `019_billing` |
| Plan → limits mapping (the enforced caps) | `internal/entitlements` |
| Stripe client (Checkout, Portal, webhook verify) — stdlib only, no SDK | `internal/stripe` |
| Billing service (read sub, start checkout/portal, apply webhook) | `internal/domain/billing` |
| `billing:read` / `billing:manage` actions | `internal/authz/policy.go` |
| Entitlement gates on create | `monitors.Create`, `statuspages.Create` |
| Org billing screen + pricing page | `web/.../orgs/[orgId]/billing`, `web/app/pricing` |

### Plans & limits

| Plan | Monitors | Status pages |
|------|----------|--------------|
| **Free** (no subscription row, or a lapsed one) | 3 | 1 |
| **Pro** (`plan='pro'` AND status `active`/`trialing`) | Unlimited | Unlimited |

Limits live in one place (`entitlements.LimitsFor`) and are checked in the
**service layer**, inside the org's `WithOrgTx`, for *any* role — they are an
entitlement, not an authz decision. The client is never trusted: a member can
create a monitor, but not past the org's plan cap (a 4th monitor on Free → **402
`plan_limit`**). An unknown/garbled plan value resolves to Free, so a bad value
can never widen entitlements.

---

## 2. How a plan changes (and why the client can't cheat)

Stripe is the **source of truth for money**. The only writer of `subscriptions`
is the signature-verified webhook:

```
Browser → POST /api/orgs/{org}/billing/checkout   (owner only)
        → returns a Stripe-hosted Checkout URL; browser redirects there
Stripe  → POST /api/stripe/webhook                 (unauthenticated; trusted ONLY by signature)
        → billing.HandleWebhook verifies HMAC, resolves the org from the event,
          and upserts the subscription row inside WithOrgTx(org) + an audit row
```

- `StartCheckout` / `StartPortal` only ever return a **redirect URL** — they
  never set a plan. No API surface lets a client declare itself Pro.
- The webhook trusts **nothing** until `stripe.VerifyWebhook` passes: it
  recomputes `HMAC-SHA256("<timestamp>.<body>", secret)` (stdlib `crypto/hmac`,
  constant-time compare) and rejects stale timestamps (replay defense). A forged
  body or wrong secret → 400, and no row is written (proven in `test/billing`).
- The org is taken from the **verified** event: `client_reference_id` on
  checkout, and `subscription.metadata.org_id` (set at checkout time) on every
  later `customer.subscription.*` event. So the webhook needs **no SECURITY
  DEFINER escape hatch** — it `WithOrgTx(org)` like any authenticated write, and
  RLS still applies. (The five existing escape-hatch functions are unchanged.)

### Invariant compliance (CLAUDE.md §2 / §5)

`subscriptions` followed the §5 recipe: `org_id` FK + index, RLS `ENABLE` +
`FORCE` + `USING`/`WITH CHECK` policy, all access via `WithOrgTx` with
belt-and-suspenders `WHERE org_id`, an audit row on every mutation, and both an
isolation case (`test/isolation`) and authz matrix rows (`test/authz`).

---

## 3. Local / test-mode setup

1. Create a Stripe **test-mode** account; create a **Product** with a recurring
   **Price** (the Pro plan). Copy the price id (`price_…`).
2. Get the test **secret key** (`sk_test_…`).
3. Run the webhook forwarder and copy the signing secret it prints (`whsec_…`):
   ```bash
   stripe listen --forward-to localhost:8080/api/stripe/webhook
   ```
4. Set in `.env` (all required together — config fails fast otherwise):
   ```
   STRIPE_SECRET_KEY=sk_test_...
   STRIPE_PRICE_ID=price_...
   STRIPE_WEBHOOK_SECRET=whsec_...
   APP_BASE_URL=http://localhost:3000
   ```
   Leave `STRIPE_SECRET_KEY` empty to run **inert**: every org is Free, and
   checkout/portal return a clear 422 instead of failing.
5. Upgrade flow: org **Billing** page → *Upgrade to Pro* → Stripe test checkout
   (use card `4242 4242 4242 4242`) → webhook flips the org to Pro.

---

## 4. Flip-to-live checklist (deferred — do NOT do this now)

Billing is intentionally **not** live. Before accepting real money:

- [ ] **Business/legal**: a real legal entity, **Terms of Service** + **Privacy
      Policy**, and a refund/cancellation policy linked from checkout & pricing.
- [ ] **Tax**: enable **Stripe Tax** (or equivalent) for VAT/GST/sales tax;
      confirm registration thresholds for your jurisdictions.
- [ ] **Stripe account**: complete activation (business details, bank payout
      account); switch from test to **live** keys.
- [ ] **Secrets**: generate **live** `STRIPE_SECRET_KEY` / `STRIPE_WEBHOOK_SECRET`,
      store them as sealed secrets (P13) — never plaintext in Git. Rotate any key
      ever committed to `.env`.
- [ ] **Live webhook endpoint**: register the production
      `https://<domain>/api/stripe/webhook` in the Stripe dashboard and subscribe
      to `checkout.session.completed`, `customer.subscription.created`,
      `customer.subscription.updated`, `customer.subscription.deleted`.
- [ ] **Idempotency/ordering**: confirm out-of-order / duplicate webhook delivery
      is safe (the upsert is idempotent; consider recording `stripe_event_id` to
      hard-dedupe if needed).
- [ ] **Dunning**: configure Stripe retries + emails for `past_due`; the app
      already downgrades entitlements to Free while not `active`/`trialing`.
- [ ] **Fraud**: enable Stripe Radar rules; rate-limit checkout creation.
- [ ] **Plan parity**: confirm the live Price matches the Pro entitlements and
      the pricing page copy.
- [ ] **Receipts/invoices**: enable Stripe customer emails (receipts, invoices).
- [ ] **Observability**: dashboard/alert on webhook failures and 4xx/5xx on
      `/api/stripe/webhook` (P14).
- [ ] **Restore drill**: confirm `subscriptions` is included in the Postgres
      backup and a restore reproduces plan state.
