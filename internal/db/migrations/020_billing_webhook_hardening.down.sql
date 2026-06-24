DROP FUNCTION IF EXISTS subscription_org_by_customer(text);
ALTER TABLE subscriptions DROP COLUMN IF EXISTS last_stripe_event_at;
