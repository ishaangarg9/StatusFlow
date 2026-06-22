-- Claim lease (Phase 6 hardening). The original claim_due_monitors (012) pushed
-- a claimed monitor's next_check_at a FULL interval ahead at claim time, so a
-- worker that crashed between claiming and persisting silently skipped that
-- monitor for an entire interval AND lost the observation. Now claiming sets a
-- SHORT lease (next_check_at = now() + lease); the worker resets next_check_at
-- to the real interval only AFTER the check is durably persisted (see runCheck).
-- A crash therefore costs at most one lease, after which the monitor is due
-- again and re-claimed — the lease IS the reaper, no separate sweeper needed.
--
-- The effective lease per row is GREATEST(p_lease_seconds, timeout_secs + 30),
-- so it always exceeds the maximum wall time of an in-flight check; a healthy
-- but slow check can never be double-claimed while it is still running.
DROP FUNCTION IF EXISTS claim_due_monitors(int);

CREATE OR REPLACE FUNCTION claim_due_monitors(p_limit int, p_lease_seconds int)
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
  SET next_check_at = now() + make_interval(
        secs => GREATEST(p_lease_seconds, (m.timeout_ms / 1000) + 30))
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

REVOKE ALL ON FUNCTION claim_due_monitors(int, int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION claim_due_monitors(int, int) TO app_user;
