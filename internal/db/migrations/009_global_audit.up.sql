-- A GLOBAL audit sink for events that must outlive the tenant they describe.
-- The per-org audit_logs table is ON DELETE CASCADE off organizations, so the
-- single most destructive action (org deletion) would erase its own trail.
-- This table has NO org FK and NO RLS (it is global, like users/sessions), so
-- a deletion record survives the cascade.
CREATE TABLE global_audit_logs (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        uuid NOT NULL,                                  -- intentionally no FK: the org may be gone
  actor_user_id uuid REFERENCES users(id) ON DELETE SET NULL,
  action        text NOT NULL,
  metadata      jsonb NOT NULL DEFAULT '{}',
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX global_audit_logs_org_time_idx ON global_audit_logs(org_id, created_at DESC);

-- app_user gets DML via the ALTER DEFAULT PRIVILEGES set in migration 007.
