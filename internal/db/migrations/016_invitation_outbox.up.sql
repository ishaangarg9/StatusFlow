-- Durable invitation delivery (Phase 6). Issuing an invite no longer sends
-- mail in-request (which left the wart: a mailer failure returned 500 *after*
-- the invitation row had already committed). Instead Create enqueues a delivery
-- and the worker drains it asynchronously, with retries.
--
-- Token model: the raw accept token is NEVER persisted. The invitation is
-- created with token_hash = NULL; the worker mints a CSPRNG token at send time,
-- stores only its sha256 in the invitation, and emails the raw value — which
-- therefore lives only transiently in worker memory. So token_hash must be
-- nullable (an invite simply isn't acceptable until delivery mints its token;
-- invitation_org_by_token matches on the hash, and NULL never matches a real
-- token's hash).
ALTER TABLE invitations ALTER COLUMN token_hash DROP NOT NULL;

-- The outbox holds NO secret — just a pointer to the invitation to be delivered,
-- plus retry bookkeeping. Tenant-owned (org_id) like every product table, so RLS
-- applies. One live delivery per invitation (UNIQUE) → a resend upserts it.
CREATE TABLE invitation_outbox (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id          uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  invitation_id   uuid NOT NULL UNIQUE REFERENCES invitations(id) ON DELETE CASCADE,
  attempts        int  NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  last_error      text,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX invitation_outbox_due_idx ON invitation_outbox(next_attempt_at);
CREATE INDEX invitation_outbox_org_idx ON invitation_outbox(org_id);

-- RLS: the backstop, same as every tenant-owned table (§5 recipe).
ALTER TABLE invitation_outbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE invitation_outbox FORCE ROW LEVEL SECURITY;
CREATE POLICY invitation_outbox_isolation ON invitation_outbox
  USING      (org_id = current_org())
  WITH CHECK (org_id = current_org());

-- The worker drains the outbox ACROSS all tenants, so — like claim_due_monitors
-- — the claim cannot be org-scoped by app.current_org_id. This SECURITY DEFINER
-- function is the sanctioned escape hatch: it locks a disjoint batch of due
-- deliveries with FOR UPDATE SKIP LOCKED (N workers never grab the same one) and
-- atomically pushes next_attempt_at out by a lease while counting the attempt,
-- so a worker that crashes mid-delivery simply retries once the lease elapses.
-- It returns the minimum the deliverer needs to mint+send (no secret leaves the
-- DB — there is none to leak). The per-delivery mint/store/send then runs inside
-- the worker's WithOrgTx(org) with RLS armed, exactly like an API write.
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
