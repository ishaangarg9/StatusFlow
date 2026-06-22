-- Restore the migration-016 claim (no accepted-invite filter; locks via the
-- outer subquery without an explicit FOR UPDATE OF target).
CREATE OR REPLACE FUNCTION claim_invitation_deliveries(p_limit int, p_lease_seconds int)
RETURNS TABLE (
  outbox_id     uuid,
  org_id        uuid,
  invitation_id uuid,
  email         text,
  role          text,
  expires_at    timestamptz,
  accepted_at   timestamptz,
  attempts      int
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = public
AS $$
  UPDATE invitation_outbox o
  SET next_attempt_at = now() + make_interval(secs => p_lease_seconds),
      attempts = o.attempts + 1
  FROM invitations i
  WHERE i.id = o.invitation_id
    AND o.id IN (
      SELECT d.id
      FROM invitation_outbox d
      WHERE d.next_attempt_at <= now()
      ORDER BY d.next_attempt_at
      LIMIT p_limit
      FOR UPDATE SKIP LOCKED
    )
  RETURNING o.id, o.org_id, o.invitation_id,
            i.email::text, i.role::text, i.expires_at, i.accepted_at, o.attempts
$$;

REVOKE ALL ON FUNCTION claim_invitation_deliveries(int, int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION claim_invitation_deliveries(int, int) TO app_user;
