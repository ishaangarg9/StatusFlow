-- "Which orgs am I in?" is a deliberately cross-tenant read: app_user runs with
-- RLS forcing org-scoping, so it cannot list a user's memberships across orgs
-- via the table directly. This SECURITY DEFINER function is the single, audited
-- escape hatch. It is owned by the privileged migration role (bypasses RLS),
-- pins search_path (anti-hijack), and filters strictly to the passed user id —
-- so a caller can only ever see their OWN memberships.
CREATE OR REPLACE FUNCTION user_memberships(p_user_id uuid)
RETURNS TABLE (
  org_id     uuid,
  org_name   text,
  org_slug   citext,
  role       org_role,
  created_at timestamptz
)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = public
AS $$
  SELECT o.id, o.name, o.slug, m.role, m.created_at
  FROM memberships m
  JOIN organizations o ON o.id = m.org_id
  WHERE m.user_id = p_user_id
  ORDER BY o.created_at
$$;

-- Lock it down: only the app role may call it; no one inherits via PUBLIC.
REVOKE ALL ON FUNCTION user_memberships(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION user_memberships(uuid) TO app_user;
