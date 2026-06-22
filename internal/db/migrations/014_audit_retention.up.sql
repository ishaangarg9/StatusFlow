-- Audit retention. Unlike the per-org read path (which runs as app_user under
-- RLS), pruning is a CROSS-tenant maintenance op: it must delete old rows from
-- every org's audit_logs plus the global sink. That is exactly the kind of
-- power app_user must never have, so this is NOT an app-facing escape hatch:
-- it is granted only to the privileged migration/admin role and is meant to be
-- run on that connection (which bypasses RLS), e.g. from pg_cron or an external
-- scheduler:  SELECT prune_audit_logs(interval '365 days');
--
-- Retention length is a parameter, not baked in — the operator owns the policy.
-- Returns how many rows each table shed so the job can log/alert on it.
CREATE OR REPLACE FUNCTION prune_audit_logs(p_retain interval)
RETURNS TABLE (org_pruned bigint, global_pruned bigint)
LANGUAGE plpgsql
AS $$
DECLARE
  o bigint;
  g bigint;
BEGIN
  IF p_retain IS NULL OR p_retain <= interval '0' THEN
    RAISE EXCEPTION 'prune_audit_logs: retention must be a positive interval (got %)', p_retain;
  END IF;
  DELETE FROM audit_logs WHERE created_at < now() - p_retain;
  GET DIAGNOSTICS o = ROW_COUNT;
  DELETE FROM global_audit_logs WHERE created_at < now() - p_retain;
  GET DIAGNOSTICS g = ROW_COUNT;
  org_pruned := o;
  global_pruned := g;
  RETURN NEXT;
END;
$$;

-- Lock it down: new functions are EXECUTE-able by PUBLIC by default, so revoke
-- that. Crucially, app_user is NOT granted EXECUTE — the app/worker can never
-- invoke a cross-tenant prune. Only the function owner (the privileged role
-- that runs migrations) can, which is the whole point.
REVOKE ALL ON FUNCTION prune_audit_logs(interval) FROM PUBLIC;
