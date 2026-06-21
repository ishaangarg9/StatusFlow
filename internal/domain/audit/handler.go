package audit

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/ishaangarg9/statusflow/internal/authz"
	"github.com/ishaangarg9/statusflow/internal/http/middleware"
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
	ac := middleware.AuthContextFrom(r.Context())
	if !authz.Can(ac, authz.ActionAuditRead, &authz.Resource{OrgID: ac.OrgID}) {
		shared.WriteErr(w, shared.Forbidden())
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	entries, err := h.svc.List(r.Context(), ac.OrgID, limit)
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"entries": entries})
}
