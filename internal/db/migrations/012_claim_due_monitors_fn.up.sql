-- The worker scans for due monitors ACROSS all tenants, so the claim query
-- cannot be scoped by app.current_org_id the way every other app_user query is.
-- Rather than grant the worker BYPASSRLS (which would weaken the strict
-- app_user role), the cross-tenant claim is wrapped in this SECURITY DEFINER
-- function — the same sanctioned escape-hatch pattern as user_memberships()
-- (008), invitation_org_by_token() (010), and public_status_page_org() (011).
--
-- It claims AND reschedules atomically: the inner SELECT locks a disjoint batch
-- of due rows with FOR UPDATE SKIP LOCKED (so N worker instances never claim
-- the same monitor), and the surrounding UPDATE immediately pushes each claimed
-- monitor's next_check_at into the future. Because claim+reschedule happen in
-- one statement, a row is invisible to a concurrent worker the instant it is
-- claimed — there is no window where two workers can both pick it up. The
-- per-monitor result is then persisted by the worker inside its own
-- WithOrgTx(org) (RLS armed), exactly like an API write.
--
-- Owned by the privileged migration role (bypasses RLS), pinned search_path,
-- EXECUTE granted only to app_user (the role the worker connects as).
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
