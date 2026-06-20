package statuspages

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ishaangarg9/statusflow/internal/shared"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// MountOrgScoped mounts the admin-side status-page routes.
// Caller must apply Authn + Tenant beforehand.
func (h *Handler) MountOrgScoped(r chi.Router) {
	r.Get("/status-pages", h.list)
	r.Post("/status-pages", h.create)
	r.Patch("/status-pages/{id}", h.update)
	r.Put("/status-pages/{id}/monitors", h.setMonitors)
}

// MountPublic mounts the public status-page endpoint. Unauthenticated.
// Resolves the org from slug only if is_public = true (doc 03 §7),
// uses a read-only tx, and emits the strict projection in doc 04 §8.
// MUST NOT return URLs, member emails, audit data, or unpublished monitors.
func (h *Handler) MountPublic(r chi.Router) {
	r.Get("/status/{slug}", h.publicView)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("GET /api/orgs/{orgId}/status-pages"))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("POST /api/orgs/{orgId}/status-pages"))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("PATCH /api/orgs/{orgId}/status-pages/{id}"))
}

func (h *Handler) setMonitors(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("PUT /api/orgs/{orgId}/status-pages/{id}/monitors"))
}

func (h *Handler) publicView(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("GET /api/public/status/{slug}"))
}
