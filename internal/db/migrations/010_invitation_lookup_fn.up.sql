-- Accepting an invite is a pre-membership action: the invitee is authenticated
-- but is NOT yet a member of the org, so RLS (which forces org-scoping for
-- app_user) cannot resolve the invitation row by its token hash through the
-- table. This SECURITY DEFINER function is the single, locked-down escape hatch
-- that turns a raw token's hash into the org it belongs to. It deliberately
-- returns ONLY the org_id (minimal disclosure) and matches on token_hash, which
-- is high-entropy and unguessable; the authoritative validation (expiry, single
-- use, email binding) then happens inside tenancy.WithOrgTx with RLS armed.
--
-- It is owned by the privileged migration role (bypasses RLS), pins search_path
-- (anti-hijack), and is callable only by app_user.
CREATE OR REPLACE FUNCTION invitation_org_by_token(p_token_hash text)
RETURNS uuid
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = public
AS $$
  SELECT org_id FROM invitations WHERE token_hash = p_token_hash
$$;

REVOKE ALL ON FUNCTION invitation_org_by_token(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION invitation_org_by_token(text) TO app_user;
