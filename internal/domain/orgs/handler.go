package orgs

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ishaangarg9/statusflow/internal/shared"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// MountGlobal mounts authenticated, non-org-scoped routes (org creation).
// Caller must apply Authn beforehand.
func (h *Handler) MountGlobal(r chi.Router) {
	r.Post("/orgs", h.create)
}

// MountOrgScoped mounts routes that operate on a specific org.
// Caller must apply Authn + Tenant beforehand.
func (h *Handler) MountOrgScoped(r chi.Router) {
	r.Get("/", h.read)
	r.Patch("/", h.update)
	r.Delete("/", h.delete)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("POST /api/orgs"))
}

func (h *Handler) read(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("GET /api/orgs/{orgId}"))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("PATCH /api/orgs/{orgId}"))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("DELETE /api/orgs/{orgId}"))
}
