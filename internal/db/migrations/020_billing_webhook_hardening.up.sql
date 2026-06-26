-- Webhook robustness (Phase 10 review fixes). Two additions, both about making
-- the (already signature-verified) Stripe webhook resilient to Stripe's at-least-
-- once, unordered delivery.

-- (1) Event ordering / idempotency. Stripe does not guarantee delivery order and
--     retries events; a stale "active" arriving (or being replayed within the
--     signature tolerance) after a "canceled" must NOT re-upgrade the org.
--     last_stripe_event_at records the `created` time of the most recently
--     APPLIED event so the upsert can refuse an older or duplicate one.
ALTER TABLE subscriptions ADD COLUMN last_stripe_event_at timestamptz;

-- (2) Customer→org fallback. A customer.subscription.* event whose metadata was
--     stripped (e.g. a subscription created in the Stripe dashboard rather than
--     through our Checkout) carries no org_id — only the Stripe customer. This
--     SECURITY DEFINER function is the sanctioned escape hatch (the 6th) for that
--     one cross-tenant lookup: with no org context, resolve the owning org from
--     the customer id we previously stored. It returns ONLY the org id (NULL when
--     unknown) and is EXECUTE-granted to app_user alone. The subscriptions_customer_idx
--     partial UNIQUE guarantees at most one org per customer, so the result is
--     unambiguous. The subsequent write still runs inside WithOrgTx with RLS
--     armed — this only fills in the org, it does not bypass the write policy.
CREATE OR REPLACE FUNCTION subscription_org_by_customer(p_customer_id text)
RETURNS uuid
LANGUAGE sql
SECURITY DEFINER
SET search_path = public
AS $$
  SELECT org_id FROM subscriptions WHERE stripe_customer_id = p_customer_id
$$;

REVOKE ALL ON FUNCTION subscription_org_by_customer(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION subscription_org_by_customer(text) TO app_user;
