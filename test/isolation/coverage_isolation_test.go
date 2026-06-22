// Package isolation_test — coverage completion for Phase 7 (the proof).
//
// The headline + product isolation files already cover monitors, invitations,
// invitation_outbox, check_results, incidents and status_pages. This file fills
// the remaining tenant-owned tables so EVERY table named in CLAUDE.md §1's claim
// has an explicit cross-tenant case: organizations, memberships, incident_updates,
// status_page_monitors, and audit_logs. Together with the others, the suite
// proves the claim "cross-tenant data leaks are structurally impossible" against
// the full schema, not a sample of it.
package isolation_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ishaangarg9/statusflow/internal/tenancy"
)

// TestOrganizationsAreOrgIsolated proves the organizations table itself is RLS
// scoped: from org A's context, a no-filter scan sees exactly org A's row and
// org B's organization is invisible (the foundation every other FK hangs off).
func TestOrganizationsAreOrgIsolated(t *testing.T) {
	ctx, pool, s := setup(t)

	var total, foreign int
	if err := tenancy.WithOrgTx(ctx, pool, s.orgA, func(tx pgx.Tx) error {
		// No WHERE — RLS is the only thing scoping this.
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM organizations`).Scan(&total); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM organizations WHERE id = $1`, s.orgB).Scan(&foreign)
	}); err != nil {
		t.Fatalf("scan organizations in org A: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected org A to see exactly its own organization, saw %d", total)
	}
	if foreign != 0 {
		t.Fatal("org B's organization row leaked into org A's context")
	}
}

// TestMembershipsAreOrgIsolated proves the membership roster is org scoped: org A
// cannot see org B's owner membership even with no app-level filter, so it can
// neither enumerate another org's members nor flip their roles.
func TestMembershipsAreOrgIsolated(t *testing.T) {
	ctx, pool, s := setup(t)

	var visible int
	if err := tenancy.WithOrgTx(ctx, pool, s.orgA, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM memberships WHERE user_id = $1`, s.userB).Scan(&visible)
	}); err != nil {
		t.Fatalf("count org B's membership from org A: %v", err)
	}
	if visible != 0 {
		t.Fatal("org B's membership leaked into org A's context")
	}

	// WITH CHECK must refuse planting a membership into org B from org A.
	if err := tenancy.WithOrgTx(ctx, pool, s.orgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO memberships (org_id, user_id, role) VALUES ($1, $2, 'admin')`,
			s.orgB, s.userA)
		return err
	}); err == nil {
		t.Fatal("expected RLS WITH CHECK to reject a membership planted into org B from org A")
	}
}

// TestIncidentUpdatesAreOrgIsolated proves an incident's timeline (the rows that
// drive the public status page narrative) obeys RLS: an update written in org A
// is invisible from org B, and WITH CHECK refuses planting one cross-org.
func TestIncidentUpdatesAreOrgIsolated(t *testing.T) {
	ctx, pool, s := setup(t)

	incID := uuid.New()
	if err := tenancy.WithOrgTx(ctx, pool, s.orgA, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO incidents (id, org_id, monitor_id, title, status)
			VALUES ($1, $2, $3, 'A is down', 'open')`, incID, s.orgA, s.monitorA); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO incident_updates (org_id, incident_id, message, status)
			VALUES ($1, $2, 'investigating', 'open')`, s.orgA, incID)
		return err
	}); err != nil {
		t.Fatalf("seed incident + update in org A: %v", err)
	}

	var visible int
	if err := tenancy.WithOrgTx(ctx, pool, s.orgB, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM incident_updates`).Scan(&visible)
	}); err != nil {
		t.Fatalf("count incident_updates in org B: %v", err)
	}
	if visible != 0 {
		t.Fatalf("org A's incident update leaked into org B (saw %d)", visible)
	}

	// WITH CHECK refuses an update planted into org B from org A's context.
	if err := tenancy.WithOrgTx(ctx, pool, s.orgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO incident_updates (org_id, incident_id, message, status)
			VALUES ($1, $2, 'evil', 'open')`, s.orgB, incID)
		return err
	}); err == nil {
		t.Fatal("expected RLS WITH CHECK to reject an incident_update planted in org B from org A")
	}
}

// TestStatusPageMonitorsAreOrgIsolated proves the join table that decides which
// monitors a status page exposes is org scoped: org A's pin is invisible from
// org B, so org B can neither read nor tamper with another org's page layout.
func TestStatusPageMonitorsAreOrgIsolated(t *testing.T) {
	ctx, pool, s := setup(t)

	pageID := uuid.New()
	slug := "iso-spm-" + uuid.NewString()[:8]
	if err := tenancy.WithOrgTx(ctx, pool, s.orgA, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO status_pages (id, org_id, slug, title, is_public)
			VALUES ($1, $2, $3, 'A', true)`, pageID, s.orgA, slug); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO status_page_monitors (status_page_id, monitor_id, org_id)
			VALUES ($1, $2, $3)`, pageID, s.monitorA, s.orgA)
		return err
	}); err != nil {
		t.Fatalf("seed status_page_monitor in org A: %v", err)
	}

	var visible int
	if err := tenancy.WithOrgTx(ctx, pool, s.orgB, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM status_page_monitors`).Scan(&visible)
	}); err != nil {
		t.Fatalf("count status_page_monitors in org B: %v", err)
	}
	if visible != 0 {
		t.Fatalf("org A's status_page_monitor leaked into org B (saw %d)", visible)
	}

	// WITH CHECK refuses pinning org A's monitor while org B is the active tenant.
	if err := tenancy.WithOrgTx(ctx, pool, s.orgB, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO status_page_monitors (status_page_id, monitor_id, org_id)
			VALUES ($1, $2, $3)`, pageID, s.monitorA, s.orgA)
		return err
	}); err == nil {
		t.Fatal("expected RLS WITH CHECK to reject a status_page_monitor planted into org A from org B")
	}
}

// TestAuditLogsAreOrgIsolated proves the audit trail — the record of every gated
// mutation — is itself tenant scoped: org A's audit rows are invisible from org B,
// and WITH CHECK refuses forging an audit row into another org. (Cross-tenant
// retention is a privileged-only path, proven separately in test/audit/.)
func TestAuditLogsAreOrgIsolated(t *testing.T) {
	ctx, pool, s := setup(t)

	if err := tenancy.WithOrgTx(ctx, pool, s.orgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO audit_logs (org_id, actor_user_id, action)
			VALUES ($1, $2, 'monitor:create')`, s.orgA, s.userA)
		return err
	}); err != nil {
		t.Fatalf("seed audit_log in org A: %v", err)
	}

	var visible int
	if err := tenancy.WithOrgTx(ctx, pool, s.orgB, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs`).Scan(&visible)
	}); err != nil {
		t.Fatalf("count audit_logs in org B: %v", err)
	}
	if visible != 0 {
		t.Fatalf("org A's audit log leaked into org B (saw %d)", visible)
	}

	// WITH CHECK refuses forging an audit row into org B from org A's context.
	if err := tenancy.WithOrgTx(ctx, pool, s.orgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO audit_logs (org_id, actor_user_id, action)
			VALUES ($1, $2, 'forged')`, s.orgB, s.userA)
		return err
	}); err == nil {
		t.Fatal("expected RLS WITH CHECK to reject an audit_log forged into org B from org A")
	}
}
