// Command seed provisions the fixed demo org that P15's "Live demo" button
// signs visitors into (see internal/demo). It creates a seed-owner account
// (used only to run the same service-layer calls a real owner would), three
// monitors against real public URLs — so the worker's real check/incident
// pipeline drives the demo instead of fabricated history — a public status
// page, and a viewer-only account for read-only visitors.
//
// Idempotent: safe to re-run. The org uses a fixed id (internal/demo.OrgID)
// so it can be looked up by primary key under RLS; users are upserted by
// email; monitors/status pages are skipped if a same-named one already exists.
//
// Run with the same DATABASE_URL as the api/worker (app_user — no privileged
// role needed; every write here goes through the normal service layer and
// tenancy.WithOrgTx, so RLS is armed exactly as it is for a real request).
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/auth"
	"github.com/ishaangarg9/statusflow/internal/authz"
	"github.com/ishaangarg9/statusflow/internal/db"
	"github.com/ishaangarg9/statusflow/internal/demo"
	"github.com/ishaangarg9/statusflow/internal/domain/monitors"
	"github.com/ishaangarg9/statusflow/internal/domain/statuspages"
	"github.com/ishaangarg9/statusflow/internal/shared"
	"github.com/ishaangarg9/statusflow/internal/tenancy"
)

type demoMonitor struct {
	name            string
	url             string
	expectedStatus  int
	intervalSeconds int
}

// Real, stable public URLs so the worker's real check/incident pipeline drives
// the demo instead of fabricated history. "Payments Service" points at a path
// that reliably 404s, so it reads as down and demonstrates the real
// incident-open pipeline within a couple of worker ticks. httpstat.us (built
// for exactly this — return an arbitrary status on demand) was tried first but
// isn't reliably reachable from every network, so plain domains are used
// instead. Kept to 3 monitors / 1 status page — the Free-plan entitlement cap —
// so the seed works unmodified against a fresh org.
var demoMonitors = []demoMonitor{
	{name: "Public Website", url: "https://example.com", expectedStatus: 200, intervalSeconds: 60},
	{name: "REST API", url: "https://api.github.com", expectedStatus: 200, intervalSeconds: 60},
	{name: "Payments Service", url: "https://example.com/nope", expectedStatus: 200, intervalSeconds: 30},
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := shared.LoadConfig()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(2)
	}

	ctx := context.Background()
	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("db pool", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	ownerID, err := ensureUser(ctx, pool, demo.OwnerEmail, "Demo Owner")
	if err != nil {
		log.Error("ensure owner", "err", err)
		os.Exit(1)
	}
	viewerID, err := ensureUser(ctx, pool, demo.ViewerEmail, "Demo Viewer")
	if err != nil {
		log.Error("ensure viewer", "err", err)
		os.Exit(1)
	}

	if err := ensureOrgAndMemberships(ctx, pool, ownerID, viewerID); err != nil {
		log.Error("ensure org", "err", err)
		os.Exit(1)
	}

	ac := authz.AuthContext{UserID: ownerID, OrgID: demo.OrgID, Role: authz.RoleOwner}

	monitorIDs, err := ensureMonitors(ctx, monitors.NewService(pool), ac)
	if err != nil {
		log.Error("ensure monitors", "err", err)
		os.Exit(1)
	}

	if err := ensureStatusPage(ctx, statuspages.NewService(pool), ac, monitorIDs); err != nil {
		log.Error("ensure status page", "err", err)
		os.Exit(1)
	}

	log.Info("seed complete",
		"org_id", demo.OrgID.String(), "org_slug", demo.OrgSlug,
		"monitors", len(monitorIDs))
}

// ensureUser upserts by email and returns the id either way. A password hash
// is computed on every run (even when the row already exists) to keep the
// insert a single atomic round trip instead of a check-then-act race; the
// password itself is random and discarded — neither account logs in with one.
func ensureUser(ctx context.Context, pool *pgxpool.Pool, email, name string) (uuid.UUID, error) {
	raw, err := auth.NewSessionToken() // 32 bytes of CSPRNG, plenty for a throwaway password
	if err != nil {
		return uuid.Nil, err
	}
	hash, err := auth.HashPassword(raw)
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, name)
		VALUES ($1, $2, $3)
		ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
		RETURNING id`,
		email, hash, name,
	).Scan(&id)
	return id, err
}

// ensureOrgAndMemberships creates the demo org (fixed id) and attaches the
// owner + viewer accounts, all inside one WithOrgTx so RLS's WITH CHECK
// (org_id = current_org()) is satisfied exactly as it is for a real request.
func ensureOrgAndMemberships(ctx context.Context, pool *pgxpool.Pool, ownerID, viewerID uuid.UUID) error {
	return tenancy.WithOrgTx(ctx, pool, demo.OrgID, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO organizations (id, name, slug)
			VALUES ($1, $2, $3)
			ON CONFLICT (id) DO NOTHING`,
			demo.OrgID, demo.OrgName, demo.OrgSlug,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO memberships (org_id, user_id, role)
			VALUES ($1, $2, 'owner')
			ON CONFLICT (org_id, user_id) DO NOTHING`,
			demo.OrgID, ownerID,
		); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO memberships (org_id, user_id, role)
			VALUES ($1, $2, 'viewer')
			ON CONFLICT (org_id, user_id) DO NOTHING`,
			demo.OrgID, viewerID,
		)
		return err
	})
}

// ensureMonitors creates any demoMonitors entry that isn't already present
// (matched by name), corrects the URL on one that is (so editing the
// demoMonitors table above and re-running fixes a bad target rather than
// requiring a manual DB reset), and returns the full set's ids.
func ensureMonitors(ctx context.Context, svc *monitors.Service, ac authz.AuthContext) ([]uuid.UUID, error) {
	existing, err := svc.List(ctx, ac.OrgID)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]monitors.MonitorView, len(existing))
	for _, m := range existing {
		byName[m.Name] = m
	}

	ids := make([]uuid.UUID, 0, len(demoMonitors))
	for _, dm := range demoMonitors {
		if m, ok := byName[dm.name]; ok {
			if m.URL != dm.url {
				updated, err := svc.Update(ctx, ac, m.ID, monitors.UpdateInput{URL: &dm.url})
				if err != nil {
					return nil, err
				}
				m = *updated
			}
			ids = append(ids, m.ID)
			continue
		}
		expected, interval := dm.expectedStatus, dm.intervalSeconds
		m, err := svc.Create(ctx, ac, monitors.CreateInput{
			Name:            dm.name,
			URL:             dm.url,
			ExpectedStatus:  &expected,
			IntervalSeconds: &interval,
		})
		if err != nil {
			return nil, err
		}
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// ensureStatusPage creates the demo status page once (matched by slug) and
// attaches every seeded monitor to it.
func ensureStatusPage(ctx context.Context, svc *statuspages.Service, ac authz.AuthContext, monitorIDs []uuid.UUID) error {
	existing, err := svc.List(ctx, ac.OrgID)
	if err != nil {
		return err
	}
	for _, sp := range existing {
		if sp.Slug == demo.OrgSlug {
			return nil
		}
	}
	_, err = svc.Create(ctx, ac, statuspages.CreateInput{
		Slug:       demo.OrgSlug,
		Title:      "StatusFlow Demo",
		IsPublic:   true,
		MonitorIDs: monitorIDs,
	})
	return err
}
