-- Billing (Phase 10). One subscription row per org tracks the plan that drives
-- server-enforced entitlements (monitor / status-page caps live in
-- internal/entitlements). Stripe is the source of truth for money; this table
-- is our local projection of it, updated only by the signature-verified webhook.
--
-- A tenant-owned table, so it follows the §5 recipe: org_id FK, index, and RLS
-- (ENABLE + FORCE + isolation policy) in this same migration. The ABSENCE of a
-- row is meaningful: an org with no subscription row is treated as Free, so we
-- never have to backfill existing orgs — the row is created lazily by the first
-- checkout webhook.
CREATE TABLE subscriptions (
  id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id                 uuid NOT NULL UNIQUE REFERENCES organizations(id) ON DELETE CASCADE,
  plan                   text NOT NULL DEFAULT 'free'
                           CHECK (plan IN ('free','pro')),
  -- Mirrors Stripe's subscription.status set so a webhook can store the value
  -- verbatim. Only 'active'/'trialing' actually entitle the paid caps
  -- (see entitlements.PlanForOrg); the rest degrade to Free.
  status                 text NOT NULL DEFAULT 'active'
                           CHECK (status IN ('active','trialing','past_due','canceled',
                                             'incomplete','incomplete_expired','unpaid','paused')),
  stripe_customer_id     text,
  stripe_subscription_id text,
  current_period_end     timestamptz,
  created_at             timestamptz NOT NULL DEFAULT now(),
  updated_at             timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX subscriptions_org_idx ON subscriptions(org_id);

-- The Stripe customer id is the join key the webhook would use as a fallback;
-- it must map to at most one org. Partial so multiple NULLs (free orgs that
-- never checked out) don't collide.
CREATE UNIQUE INDEX subscriptions_customer_idx
  ON subscriptions(stripe_customer_id) WHERE stripe_customer_id IS NOT NULL;

-- RLS: the backstop, same as every tenant-owned table (§5 recipe). The webhook
-- resolves the org from the event (client_reference_id / subscription metadata)
-- and then writes inside WithOrgTx with RLS armed, exactly like an API write —
-- so no SECURITY DEFINER escape hatch is needed here.
ALTER TABLE subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE subscriptions FORCE ROW LEVEL SECURITY;
CREATE POLICY subscriptions_isolation ON subscriptions
  USING      (org_id = current_org())
  WITH CHECK (org_id = current_org());
