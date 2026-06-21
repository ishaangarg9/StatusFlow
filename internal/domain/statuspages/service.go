package statuspages

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/authz"
	"github.com/ishaangarg9/statusflow/internal/domain/audit"
	"github.com/ishaangarg9/statusflow/internal/shared"
	"github.com/ishaangarg9/statusflow/internal/tenancy"
)

// Service hosts admin-side and public-side status-page logic.
//
// Public-side rules (doc 04 §8, doc 06 ADR-07):
//   - Resolve org from slug ONLY if is_public = true; otherwise 404.
//   - Open WithOrgTx(org) for a READ-ONLY view; emit the strict projection.
//   - Never select monitored URL, member emails, audit data, or unpublished
//     monitors into the response. Treat this as a hardened, separate code path.
type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// slugRe constrains a slug to a DNS-label-ish shape so it is safe to surface in
// a public URL and predictable to look up.
var slugRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// StatusPageView is the admin-side projection (the public path uses PublicView).
type StatusPageView struct {
	ID         uuid.UUID   `json:"id"`
	Slug       string      `json:"slug"`
	Title      string      `json:"title"`
	IsPublic   bool        `json:"isPublic"`
	CreatedAt  time.Time   `json:"createdAt"`
	MonitorIDs []uuid.UUID `json:"monitorIds"`
}

// Public projection types — the strict, hardened shape from doc 04 §8.
type PublicView struct {
	Title      string            `json:"title"`
	Overall    string            `json:"overall"`
	Components []PublicComponent `json:"components"`
	Incidents  []PublicIncident  `json:"incidents"`
}

type PublicComponent struct {
	Name   string `json:"name"`
	Status string `json:"status"` // operational | down
}

type PublicIncident struct {
	Title     string                 `json:"title"`
	Status    string                 `json:"status"`
	StartedAt time.Time              `json:"startedAt"`
	Updates   []PublicIncidentUpdate `json:"updates"`
}

type PublicIncidentUpdate struct {
	Message   string    `json:"message"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

type CreateInput struct {
	Slug       string
	Title      string
	IsPublic   bool
	MonitorIDs []uuid.UUID
}

type UpdateInput struct {
	Title    *string
	IsPublic *bool
}

// List returns the org's status pages with their attached monitor ids.
func (s *Service) List(ctx context.Context, orgID uuid.UUID) ([]StatusPageView, error) {
	out := []StatusPageView{}
	err := tenancy.WithOrgTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, slug, title, is_public, created_at
			FROM status_pages
			WHERE org_id = $1
			ORDER BY created_at DESC`, orgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		idx := map[uuid.UUID]int{} // page id -> index into out
		for rows.Next() {
			var v StatusPageView
			if err := rows.Scan(&v.ID, &v.Slug, &v.Title, &v.IsPublic, &v.CreatedAt); err != nil {
				return err
			}
			v.MonitorIDs = []uuid.UUID{}
			out = append(out, v)
			idx[v.ID] = len(out) - 1
		}
		if err := rows.Err(); err != nil {
			return err
		}
		// Attach monitor ids in one extra query. Index by position (not a pointer
		// into out) so appends to out can't invalidate references.
		mrows, err := tx.Query(ctx, `
			SELECT status_page_id, monitor_id
			FROM status_page_monitors
			WHERE org_id = $1
			ORDER BY monitor_id`, orgID)
		if err != nil {
			return err
		}
		defer mrows.Close()
		for mrows.Next() {
			var pageID, monitorID uuid.UUID
			if err := mrows.Scan(&pageID, &monitorID); err != nil {
				return err
			}
			if i, ok := idx[pageID]; ok {
				out[i].MonitorIDs = append(out[i].MonitorIDs, monitorID)
			}
		}
		return mrows.Err()
	})
	if err != nil {
		return nil, shared.MapAppErr(err)
	}
	return out, nil
}

// Create inserts a status page and attaches the requested monitors. Only
// monitors that belong to the active org are attachable — a foreign or unknown
// id is rejected as 422. A slug collision (slugs are globally unique) is 409.
func (s *Service) Create(ctx context.Context, ac authz.AuthContext, in CreateInput) (*StatusPageView, error) {
	if !authz.Can(ac, authz.ActionStatusPublish, &authz.Resource{OrgID: ac.OrgID}) {
		return nil, shared.Forbidden()
	}
	slug := strings.ToLower(strings.TrimSpace(in.Slug))
	if !slugRe.MatchString(slug) {
		return nil, shared.Validation("Slug must be lowercase letters, digits, and hyphens.")
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, shared.Validation("A title is required.")
	}

	var v StatusPageView
	v.MonitorIDs = []uuid.UUID{}
	err := tenancy.WithOrgTx(ctx, s.pool, ac.OrgID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO status_pages (org_id, slug, title, is_public)
			VALUES ($1, $2, $3, $4)
			RETURNING id, slug, title, is_public, created_at`,
			ac.OrgID, slug, title, in.IsPublic,
		).Scan(&v.ID, &v.Slug, &v.Title, &v.IsPublic, &v.CreatedAt); err != nil {
			if shared.IsUniqueViolation(err) {
				return shared.Conflict("That slug is already taken.")
			}
			return err
		}
		attached, err := setMonitors(ctx, tx, ac.OrgID, v.ID, in.MonitorIDs)
		if err != nil {
			return err
		}
		v.MonitorIDs = attached
		return audit.Record(ctx, tx, audit.Entry{
			OrgID:        ac.OrgID,
			ActorUserID:  ac.UserID,
			Action:       "statuspage:publish",
			ResourceType: "status_page",
			ResourceID:   &v.ID,
			Metadata:     map[string]any{"slug": slug, "isPublic": in.IsPublic},
		})
	})
	if err != nil {
		return nil, shared.MapAppErr(err)
	}
	return &v, nil
}

// Update patches a status page's title and/or visibility.
func (s *Service) Update(ctx context.Context, ac authz.AuthContext, id uuid.UUID, in UpdateInput) (*StatusPageView, error) {
	if !authz.Can(ac, authz.ActionStatusPublish, &authz.Resource{OrgID: ac.OrgID}) {
		return nil, shared.Forbidden()
	}
	if in.Title != nil && strings.TrimSpace(*in.Title) == "" {
		return nil, shared.Validation("A title cannot be empty.")
	}
	var v StatusPageView
	v.MonitorIDs = []uuid.UUID{}
	err := tenancy.WithOrgTx(ctx, s.pool, ac.OrgID, func(tx pgx.Tx) error {
		var title *string
		if in.Title != nil {
			t := strings.TrimSpace(*in.Title)
			title = &t
		}
		if err := tx.QueryRow(ctx, `
			UPDATE status_pages SET
				title     = COALESCE($3, title),
				is_public = COALESCE($4, is_public)
			WHERE org_id = $1 AND id = $2
			RETURNING id, slug, title, is_public, created_at`,
			ac.OrgID, id, title, in.IsPublic,
		).Scan(&v.ID, &v.Slug, &v.Title, &v.IsPublic, &v.CreatedAt); err != nil {
			return err // ErrNoRows -> 404
		}
		attached, err := loadMonitorIDs(ctx, tx, ac.OrgID, id)
		if err != nil {
			return err
		}
		v.MonitorIDs = attached
		return audit.Record(ctx, tx, audit.Entry{
			OrgID:        ac.OrgID,
			ActorUserID:  ac.UserID,
			Action:       "statuspage:publish",
			ResourceType: "status_page",
			ResourceID:   &id,
		})
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.NotFound()
		}
		return nil, shared.MapAppErr(err)
	}
	return &v, nil
}

// SetMonitors replaces the full set of monitors attached to a page.
func (s *Service) SetMonitors(ctx context.Context, ac authz.AuthContext, id uuid.UUID, monitorIDs []uuid.UUID) (*StatusPageView, error) {
	if !authz.Can(ac, authz.ActionStatusPublish, &authz.Resource{OrgID: ac.OrgID}) {
		return nil, shared.Forbidden()
	}
	var v StatusPageView
	err := tenancy.WithOrgTx(ctx, s.pool, ac.OrgID, func(tx pgx.Tx) error {
		// Confirm the page is in this org first (404 otherwise).
		if err := tx.QueryRow(ctx, `
			SELECT id, slug, title, is_public, created_at
			FROM status_pages WHERE org_id = $1 AND id = $2`, ac.OrgID, id,
		).Scan(&v.ID, &v.Slug, &v.Title, &v.IsPublic, &v.CreatedAt); err != nil {
			return err // ErrNoRows -> 404
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM status_page_monitors WHERE org_id = $1 AND status_page_id = $2`,
			ac.OrgID, id); err != nil {
			return err
		}
		attached, err := setMonitors(ctx, tx, ac.OrgID, id, monitorIDs)
		if err != nil {
			return err
		}
		v.MonitorIDs = attached
		return audit.Record(ctx, tx, audit.Entry{
			OrgID:        ac.OrgID,
			ActorUserID:  ac.UserID,
			Action:       "statuspage:publish",
			ResourceType: "status_page",
			ResourceID:   &id,
			Metadata:     map[string]any{"monitorCount": len(attached)},
		})
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.NotFound()
		}
		return nil, shared.MapAppErr(err)
	}
	return &v, nil
}

// setMonitors attaches the given monitor ids to a page, accepting only ids that
// belong to the active org. If any requested id is foreign/unknown, it returns
// a 422 so the caller can't silently attach a partial set. Assumes any prior
// rows for this page were already cleared by the caller (Create starts empty).
func setMonitors(ctx context.Context, tx pgx.Tx, orgID, pageID uuid.UUID, monitorIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(monitorIDs) == 0 {
		return []uuid.UUID{}, nil
	}
	// Insert only monitors that live in this org. RLS on monitors + the explicit
	// org_id filter mean a foreign id contributes no row.
	tag, err := tx.Exec(ctx, `
		INSERT INTO status_page_monitors (status_page_id, monitor_id, org_id)
		SELECT $1, m.id, $2 FROM monitors m
		WHERE m.org_id = $2 AND m.id = ANY($3)
		ON CONFLICT DO NOTHING`,
		pageID, orgID, monitorIDs)
	if err != nil {
		return nil, err
	}
	if int(tag.RowsAffected()) != len(dedupe(monitorIDs)) {
		return nil, shared.Validation("One or more monitorIds are not monitors in this organization.")
	}
	return loadMonitorIDs(ctx, tx, orgID, pageID)
}

func loadMonitorIDs(ctx context.Context, tx pgx.Tx, orgID, pageID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx,
		`SELECT monitor_id FROM status_page_monitors WHERE org_id = $1 AND status_page_id = $2 ORDER BY monitor_id`,
		orgID, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func dedupe(ids []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := ids[:0:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// PublicView serves the unauthenticated status page for a slug. The org is
// resolved via the locked-down public_status_page_org() function (which returns
// a row only when the page is public); everything else is read inside
// WithOrgTx(org) under RLS. The strict projection NEVER includes monitor URLs,
// member identities, or audit data. An unknown/private slug is 404.
func (s *Service) PublicView(ctx context.Context, slug string) (*PublicView, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return nil, shared.NotFound()
	}

	var orgPtr *uuid.UUID
	if err := s.pool.QueryRow(ctx,
		`SELECT public_status_page_org($1)`, slug).Scan(&orgPtr); err != nil {
		return nil, shared.Internal(err)
	}
	if orgPtr == nil {
		return nil, shared.NotFound() // unknown slug or not public — hide existence
	}
	orgID := *orgPtr

	view := &PublicView{Components: []PublicComponent{}, Incidents: []PublicIncident{}}
	err := tenancy.WithOrgTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		var pageID uuid.UUID
		// Re-read under RLS for the title + id (and re-confirm is_public).
		if err := tx.QueryRow(ctx, `
			SELECT id, title FROM status_pages
			WHERE org_id = $1 AND slug = $2 AND is_public = true`, orgID, slug,
		).Scan(&pageID, &view.Title); err != nil {
			return err // ErrNoRows -> 404
		}

		// Components: each attached monitor's NAME (never its URL) plus a status
		// derived from its latest check. No checks yet → treated as operational.
		crows, err := tx.Query(ctx, `
			SELECT m.name, latest.status
			FROM status_page_monitors spm
			JOIN monitors m ON m.id = spm.monitor_id AND m.org_id = $1
			LEFT JOIN LATERAL (
				SELECT status FROM check_results cr
				WHERE cr.monitor_id = m.id AND cr.org_id = $1
				ORDER BY cr.checked_at DESC
				LIMIT 1
			) latest ON true
			WHERE spm.org_id = $1 AND spm.status_page_id = $2
			ORDER BY m.name`, orgID, pageID)
		if err != nil {
			return err
		}
		defer crows.Close()
		var downCount int
		for crows.Next() {
			var name string
			var status *string
			if err := crows.Scan(&name, &status); err != nil {
				return err
			}
			compStatus := "operational"
			if status != nil && *status == "down" {
				compStatus = "down"
				downCount++
			}
			view.Components = append(view.Components, PublicComponent{Name: name, Status: compStatus})
		}
		if err := crows.Err(); err != nil {
			return err
		}
		view.Overall = deriveOverall(len(view.Components), downCount)

		// Open incidents for monitors attached to this page, with their updates.
		irows, err := tx.Query(ctx, `
			SELECT i.id, i.title, i.status, i.started_at
			FROM incidents i
			JOIN status_page_monitors spm ON spm.monitor_id = i.monitor_id AND spm.org_id = $1
			WHERE i.org_id = $1 AND spm.status_page_id = $2 AND i.status = 'open'
			ORDER BY i.started_at DESC`, orgID, pageID)
		if err != nil {
			return err
		}
		defer irows.Close()
		type inc struct {
			id  uuid.UUID
			pub PublicIncident
		}
		var incs []inc
		for irows.Next() {
			var it inc
			it.pub.Updates = []PublicIncidentUpdate{}
			if err := irows.Scan(&it.id, &it.pub.Title, &it.pub.Status, &it.pub.StartedAt); err != nil {
				return err
			}
			incs = append(incs, it)
		}
		if err := irows.Err(); err != nil {
			return err
		}
		for i := range incs {
			urows, err := tx.Query(ctx, `
				SELECT message, status, created_at
				FROM incident_updates
				WHERE org_id = $1 AND incident_id = $2
				ORDER BY created_at`, orgID, incs[i].id)
			if err != nil {
				return err
			}
			func() {
				defer urows.Close()
				for urows.Next() {
					var u PublicIncidentUpdate
					if err = urows.Scan(&u.Message, &u.Status, &u.CreatedAt); err != nil {
						return
					}
					incs[i].pub.Updates = append(incs[i].pub.Updates, u)
				}
				if e := urows.Err(); e != nil && err == nil {
					err = e
				}
			}()
			if err != nil {
				return err
			}
			view.Incidents = append(view.Incidents, incs[i].pub)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.NotFound()
		}
		return nil, shared.MapAppErr(err)
	}
	return view, nil
}

// deriveOverall maps component health to the page-level status word.
func deriveOverall(total, down int) string {
	switch {
	case down == 0:
		return "operational"
	case down >= total:
		return "down"
	default:
		return "degraded"
	}
}
