-- Row-Level Security: the backstop. Every tenant-owned table gets
-- ENABLE + FORCE RLS plus an isolation policy on org_id = current_org().
-- USING governs read visibility; WITH CHECK governs writes.

-- organizations: a user only ever operates inside one active org per request.
ALTER TABLE organizations ENABLE ROW LEVEL SECURITY;
ALTER TABLE organizations FORCE ROW LEVEL SECURITY;
CREATE POLICY organizations_isolation ON organizations
  USING      (id = current_org())
  WITH CHECK (id = current_org());

ALTER TABLE memberships ENABLE ROW LEVEL SECURITY;
ALTER TABLE memberships FORCE ROW LEVEL SECURITY;
CREATE POLICY memberships_isolation ON memberships
  USING      (org_id = current_org())
  WITH CHECK (org_id = current_org());

ALTER TABLE invitations ENABLE ROW LEVEL SECURITY;
ALTER TABLE invitations FORCE ROW LEVEL SECURITY;
CREATE POLICY invitations_isolation ON invitations
  USING      (org_id = current_org())
  WITH CHECK (org_id = current_org());

ALTER TABLE monitors ENABLE ROW LEVEL SECURITY;
ALTER TABLE monitors FORCE ROW LEVEL SECURITY;
CREATE POLICY monitors_isolation ON monitors
  USING      (org_id = current_org())
  WITH CHECK (org_id = current_org());

ALTER TABLE check_results ENABLE ROW LEVEL SECURITY;
ALTER TABLE check_results FORCE ROW LEVEL SECURITY;
CREATE POLICY check_results_isolation ON check_results
  USING      (org_id = current_org())
  WITH CHECK (org_id = current_org());

ALTER TABLE incidents ENABLE ROW LEVEL SECURITY;
ALTER TABLE incidents FORCE ROW LEVEL SECURITY;
CREATE POLICY incidents_isolation ON incidents
  USING      (org_id = current_org())
  WITH CHECK (org_id = current_org());

ALTER TABLE incident_updates ENABLE ROW LEVEL SECURITY;
ALTER TABLE incident_updates FORCE ROW LEVEL SECURITY;
CREATE POLICY incident_updates_isolation ON incident_updates
  USING      (org_id = current_org())
  WITH CHECK (org_id = current_org());

ALTER TABLE status_pages ENABLE ROW LEVEL SECURITY;
ALTER TABLE status_pages FORCE ROW LEVEL SECURITY;
CREATE POLICY status_pages_isolation ON status_pages
  USING      (org_id = current_org())
  WITH CHECK (org_id = current_org());

ALTER TABLE status_page_monitors ENABLE ROW LEVEL SECURITY;
ALTER TABLE status_page_monitors FORCE ROW LEVEL SECURITY;
CREATE POLICY status_page_monitors_isolation ON status_page_monitors
  USING      (org_id = current_org())
  WITH CHECK (org_id = current_org());

ALTER TABLE audit_logs ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_logs FORCE ROW LEVEL SECURITY;
CREATE POLICY audit_logs_isolation ON audit_logs
  USING      (org_id = current_org())
  WITH CHECK (org_id = current_org());
