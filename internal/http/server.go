// Package http wires the API surface: chi router + middleware stack + the
// domain handlers mounted at the paths from doc 04. Everything authn / authz /
// tenant-related lives in middleware/.
package http

import (
	"log/slog"
	nethttp "net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/auth"
	"github.com/ishaangarg9/statusflow/internal/domain/audit"
	"github.com/ishaangarg9/statusflow/internal/domain/incidents"
	"github.com/ishaangarg9/statusflow/internal/domain/invitations"
	"github.com/ishaangarg9/statusflow/internal/domain/memberships"
	"github.com/ishaangarg9/statusflow/internal/domain/monitors"
	"github.com/ishaangarg9/statusflow/internal/domain/orgs"
	"github.com/ishaangarg9/statusflow/internal/domain/statuspages"
	"github.com/ishaangarg9/statusflow/internal/domain/users"
	"github.com/ishaangarg9/statusflow/internal/http/middleware"
	"github.com/ishaangarg9/statusflow/internal/shared"
)

type Server struct {
	cfg      *shared.Config
	pool     *pgxpool.Pool
	sessions *auth.SessionStore
	log      *slog.Logger
}

func NewServer(cfg *shared.Config, pool *pgxpool.Pool, sessions *auth.SessionStore, log *slog.Logger) *Server {
	return &Server{cfg: cfg, pool: pool, sessions: sessions, log: log}
}

// Routes builds the full router. Route table follows doc 04.
//
// Layering — read top to bottom:
//
//	/api/public/*            unauthenticated; strict projection only
//	/api/auth/{signup,login} unauthenticated; per-IP rate-limited
//	/api/auth/* (rest)       Authn required
//	/api/orgs                Authn required (POST creates an org)
//	/api/invitations/accept  Authn required (invitee not yet a member)
//	/api/orgs/{orgId}/*      Authn + Tenant (membership) required
func (s *Server) Routes() nethttp.Handler {
	// Domain wiring
	usersH := users.NewHandler(users.NewService(s.pool), s.sessions)
	orgsH := orgs.NewHandler(orgs.NewService(s.pool))
	membersH := memberships.NewHandler(memberships.NewService(s.pool))
	invH := invitations.NewHandler(invitations.NewService(s.pool))
	monH := monitors.NewHandler(monitors.NewService(s.pool))
	incH := incidents.NewHandler(incidents.NewService(s.pool))
	spH := statuspages.NewHandler(statuspages.NewService(s.pool))
	audH := audit.NewHandler(audit.NewService(s.pool))

	r := chi.NewRouter()
	r.Use(chimw.Recoverer)
	r.Use(middleware.RequestContext(s.log))

	// Public, unauthenticated surface — hardened, read-only projection.
	r.Route("/api/public", func(r chi.Router) {
		spH.MountPublic(r)
	})

	// Auth endpoints.
	r.Route("/api/auth", func(r chi.Router) {
		// Public (signup, login) — per-IP rate limited.
		r.Group(func(r chi.Router) {
			r.Use(middleware.IPRateLimit(s.cfg.RateLimitLoginPerMin))
			usersH.MountPublic(r)
		})
		// Authenticated.
		r.Group(func(r chi.Router) {
			r.Use(middleware.Authn(s.sessions))
			usersH.MountAuthenticated(r)
		})
	})

	// Authenticated, non-org-scoped surface.
	r.Route("/api", func(r chi.Router) {
		r.Use(middleware.Authn(s.sessions))

		orgsH.MountGlobal(r)
		invH.MountGlobal(r)

		// Org-scoped routes — Tenant middleware pins active org via membership.
		r.Route("/orgs/{orgId}", func(r chi.Router) {
			r.Use(middleware.Tenant(s.pool))
			orgsH.MountOrgScoped(r)
			membersH.Mount(r)
			invH.MountOrgScoped(r)
			monH.Mount(r)
			incH.Mount(r)
			spH.MountOrgScoped(r)
			audH.Mount(r)
		})
	})

	return r
}
