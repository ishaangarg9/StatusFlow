-- Refine the delivery claim (Phase 6 review fix):
--  1. Skip outbox rows whose invitation is already accepted. Re-minting a token
--     for an accepted invite would email a fresh, valid link for an org the user
--     already joined. (Accept also deletes the outbox row, so this is
--     belt-and-suspenders against an accept/claim race.) Expired-but-unaccepted
--     rows are still claimed so process() can reap them.
--  2. Lock only the outbox row (FOR UPDATE OF d), never the joined invitations
--     row, so claiming can't block a concurrent Accept's FOR UPDATE.
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
      JOIN invitations di ON di.id = d.invitation_id
      WHERE d.next_attempt_at <= now()
        AND di.accepted_at IS NULL
      ORDER BY d.next_attempt_at
      LIMIT p_limit
      FOR UPDATE OF d SKIP LOCKED
    )
  RETURNING o.id, o.org_id, o.invitation_id,
            i.email::text, i.role::text, i.expires_at, i.accepted_at, o.attempts
$$;

REVOKE ALL ON FUNCTION claim_invitation_deliveries(int, int) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION claim_invitation_deliveries(int, int) TO app_user;
