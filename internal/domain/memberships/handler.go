package memberships

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ishaangarg9/statusflow/internal/shared"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount mounts org-scoped member-management routes.
// Caller must apply Authn + Tenant beforehand.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/members", h.list)
	r.Patch("/members/{userId}", h.updateRole)
	r.Delete("/members/{userId}", h.remove)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("GET /api/orgs/{orgId}/members"))
}

func (h *Handler) updateRole(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("PATCH /api/orgs/{orgId}/members/{userId}"))
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("DELETE /api/orgs/{orgId}/members/{userId}"))
}
