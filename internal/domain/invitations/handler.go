package invitations

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ishaangarg9/statusflow/internal/shared"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// MountGlobal mounts the global invite-accept endpoint.
// Caller must apply Authn beforehand (invitee must be signed in).
func (h *Handler) MountGlobal(r chi.Router) {
	r.Post("/invitations/accept", h.accept)
}

// MountOrgScoped mounts org-scoped invite routes.
// Caller must apply Authn + Tenant beforehand.
func (h *Handler) MountOrgScoped(r chi.Router) {
	r.Get("/invitations", h.list)
	r.Post("/invitations", h.create)
	r.Delete("/invitations/{id}", h.revoke)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("GET /api/orgs/{orgId}/invitations"))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("POST /api/orgs/{orgId}/invitations"))
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("DELETE /api/orgs/{orgId}/invitations/{id}"))
}

func (h *Handler) accept(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("POST /api/invitations/accept"))
}
