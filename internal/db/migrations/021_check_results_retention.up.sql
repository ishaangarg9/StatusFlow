-- check_results retention. Every monitor check inserts a row forever, so this
-- table dwarfs every other one over time. Mirrors the audit-retention pattern
-- (014 + 018): pruning is a CROSS-tenant maintenance op — it deletes stale rows
-- across every org — which is exactly the power app_user must never hold. So it
-- is NOT an app-facing escape hatch: EXECUTE is revoked from PUBLIC and never
-- granted to app_user; only the privileged migration/admin role (which bypasses
-- RLS) can invoke it, e.g. from pg_cron:  SELECT prune_check_results(interval '90 days');
--
-- Retention length is a parameter — the operator owns the policy. Returns how
-- many rows were shed so the job can log/alert on it.
CREATE OR REPLACE FUNCTION prune_check_results(p_retain interval)
RETURNS bigint
LANGUAGE plpgsql
AS $$
DECLARE
  n bigint;
BEGIN
  IF p_retain IS NULL OR p_retain <= interval '0' THEN
    RAISE EXCEPTION 'prune_check_results: retention must be a positive interval (got %)', p_retain;
  END IF;
  DELETE FROM check_results WHERE checked_at < now() - p_retain;
  GET DIAGNOSTICS n = ROW_COUNT;
  RETURN n;
END;
$$;

-- Lock it down: new functions are EXECUTE-able by PUBLIC by default. Revoke that;
-- crucially app_user is NOT granted EXECUTE — the app/worker can never invoke a
-- cross-tenant prune. Only the function owner (the privileged migration role) can.
REVOKE ALL ON FUNCTION prune_check_results(interval) FROM PUBLIC;

-- Schedule it where pg_cron is available (Supabase / most managed Postgres), on
-- the privileged connection that owns the function. Where pg_cron is absent
-- (e.g. local docker) this is a no-op and the operator schedules it externally.
-- cron.schedule upserts by job name, so re-running this migration is idempotent.
-- 90 days is the default policy for high-volume check data; adjust to taste.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_cron') THEN
    PERFORM cron.schedule(
      'statusflow-prune-check-results',
      '42 3 * * *',
      $cron$SELECT prune_check_results(interval '90 days')$cron$
    );
  END IF;
END $$;
