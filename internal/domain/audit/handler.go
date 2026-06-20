package audit

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ishaangarg9/statusflow/internal/shared"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Mount mounts org-scoped audit log routes.
// Caller must apply Authn + Tenant beforehand.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/audit", h.list)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("GET /api/orgs/{orgId}/audit"))
}
