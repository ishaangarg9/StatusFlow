-- Restore the migration-012 signature: claim sets next_check_at a full interval
-- ahead at claim time (no lease).
DROP FUNCTION IF EXISTS claim_due_monitors(int, int);

CREATE OR REPLACE FUNCTION claim_due_monitors(p_limit int)
RETURNS TABLE (
  id               uuid,
  org_id           uuid,
  url              text,
  method           text,
  timeout_ms       int,
  interval_seconds int,
  expected_status  int
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = public
AS $$
  UPDATE monitors m
  SET next_check_at = now() + make_interval(secs => m.interval_seconds)
  WHERE m.id IN (
    SELECT d.id
    FROM monitors d
    WHERE d.is_paused = false AND d.next_check_at <= now()
    ORDER BY d.next_check_at
    LIMIT p_limit
    FOR UPDATE SKIP LOCKED
  )
  RETURNING m.id, m.org_id, m.url, m.method, m.timeout_ms, m.interval_seconds, m.expected_status
$$;

REVOKE ALL ON FUNCTION claim_due_monitors(int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION claim_due_monitors(int) TO app_user;
