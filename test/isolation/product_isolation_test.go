package isolation_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ishaangarg9/statusflow/internal/tenancy"
)

// TestCheckResultsAreOrgIsolated proves a monitor's check history written in
// org A is invisible from org B even with no app-level org filter — the worker
// pins app.current_org_id per monitor's org, so its writes obey the same RLS.
func TestCheckResultsAreOrgIsolated(t *testing.T) {
	ctx, pool, s := setup(t)

	if err := tenancy.WithOrgTx(ctx, pool, s.orgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO check_results (org_id, monitor_id, status, status_code, latency_ms)
			VALUES ($1, $2, 'up', 200, 12)`, s.orgA, s.monitorA)
		return err
	}); err != nil {
		t.Fatalf("seed check_result in org A: %v", err)
	}

	var visible int
	if err := tenancy.WithOrgTx(ctx, pool, s.orgB, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM check_results`).Scan(&visible)
	}); err != nil {
		t.Fatalf("count check_results in org B: %v", err)
	}
	if visible != 0 {
		t.Fatalf("org A's check results leaked into org B (saw %d)", visible)
	}
}

// TestIncidentsAreOrgIsolated proves an incident opened in org A is invisible
// from org B, and that WITH CHECK refuses planting an incident in org B from
// org A's context.
func TestIncidentsAreOrgIsolated(t *testing.T) {
	ctx, pool, s := setup(t)

	if err := tenancy.WithOrgTx(ctx, pool, s.orgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO incidents (org_id, monitor_id, title, status)
			VALUES ($1, $2, 'A is down', 'open')`, s.orgA, s.monitorA)
		return err
	}); err != nil {
		t.Fatalf("seed incident in org A: %v", err)
	}

	var visible int
	if err := tenancy.WithOrgTx(ctx, pool, s.orgB, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM incidents`).Scan(&visible)
	}); err != nil {
		t.Fatalf("count incidents in org B: %v", err)
	}
	if visible != 0 {
		t.Fatalf("org A's incident leaked into org B (saw %d)", visible)
	}

	// WITH CHECK must refuse an incident for org B's monitor while org A is active.
	if err := tenancy.WithOrgTx(ctx, pool, s.orgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO incidents (org_id, monitor_id, title, status)
			VALUES ($1, $2, 'evil', 'open')`, s.orgB, s.monitorB)
		return err
	}); err == nil {
		t.Fatal("expected RLS WITH CHECK to reject an incident planted in org B from org A")
	}
}

// TestStatusPagesAreOrgIsolated proves a status page created in org A is
// invisible from org B's context.
func TestStatusPagesAreOrgIsolated(t *testing.T) {
	ctx, pool, s := setup(t)

	slug := "iso-page-" + uuid.NewString()[:8]
	if err := tenancy.WithOrgTx(ctx, pool, s.orgA, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO status_pages (org_id, slug, title, is_public)
			VALUES ($1, $2, 'A', true)`, s.orgA, slug)
		return err
	}); err != nil {
		t.Fatalf("seed status page in org A: %v", err)
	}

	var visible int
	if err := tenancy.WithOrgTx(ctx, pool, s.orgB, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM status_pages`).Scan(&visible)
	}); err != nil {
		t.Fatalf("count status pages in org B: %v", err)
	}
	if visible != 0 {
		t.Fatalf("org A's status page leaked into org B (saw %d)", visible)
	}
}

// TestPublicStatusPageFnRespectsVisibility guards the public_status_page_org()
// SECURITY DEFINER escape hatch: it must resolve only PUBLIC pages, return the
// owning org for those, and reveal nothing about a private page (NULL).
func TestPublicStatusPageFnRespectsVisibility(t *testing.T) {
	ctx, pool, s := setup(t)

	publicSlug := "pub-" + uuid.NewString()[:8]
	privateSlug := "priv-" + uuid.NewString()[:8]
	if err := tenancy.WithOrgTx(ctx, pool, s.orgA, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO status_pages (org_id, slug, title, is_public) VALUES ($1, $2, 'pub', true)`,
			s.orgA, publicSlug); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO status_pages (org_id, slug, title, is_public) VALUES ($1, $2, 'priv', false)`,
			s.orgA, privateSlug)
		return err
	}); err != nil {
		t.Fatalf("seed status pages: %v", err)
	}

	// Public slug resolves to its org (called on the bare pool, as the public
	// handler does — no membership context).
	var org *uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT public_status_page_org($1)`, publicSlug).Scan(&org); err != nil {
		t.Fatalf("public_status_page_org(public): %v", err)
	}
	if org == nil || *org != s.orgA {
		t.Fatalf("expected public slug to resolve to org A, got %v", org)
	}

	// Private slug must reveal nothing.
	var priv *uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT public_status_page_org($1)`, privateSlug).Scan(&priv); err != nil {
		t.Fatalf("public_status_page_org(private): %v", err)
	}
	if priv != nil {
		t.Fatal("a private status page was exposed through public_status_page_org")
	}
}
