-- The public status page is served to UNAUTHENTICATED visitors, so there is no
-- session, no membership, and therefore no app.current_org_id for RLS to scope
-- by. This SECURITY DEFINER function is the single, locked-down escape hatch
-- that maps a public page's slug to the org it belongs to -- and ONLY when the
-- page is public. It deliberately returns just the org_id (minimal disclosure);
-- the actual projection (title, component statuses, incidents) is then read
-- inside tenancy.WithOrgTx(org) with RLS armed, exactly like the authenticated
-- paths. An unknown slug, or a slug for a non-public page, yields SQL NULL so
-- the existence of a private page is never revealed (404 either way).
--
-- Same hardening as invitation_org_by_token (migration 010): owned by the
-- privileged migration role (bypasses RLS), pinned search_path, EXECUTE granted
-- only to app_user.
CREATE OR REPLACE FUNCTION public_status_page_org(p_slug citext)
RETURNS uuid
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = public
AS $$
  SELECT org_id FROM status_pages WHERE slug = p_slug AND is_public = true
$$;

REVOKE ALL ON FUNCTION public_status_page_org(citext) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public_status_page_org(citext) TO app_user;
