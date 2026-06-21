package memberships

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ishaangarg9/statusflow/internal/authz"
	"github.com/ishaangarg9/statusflow/internal/http/middleware"
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
	ac := middleware.AuthContextFrom(r.Context())
	if !authz.Can(ac, authz.ActionMemberRead, &authz.Resource{OrgID: ac.OrgID}) {
		shared.WriteErr(w, shared.Forbidden())
		return
	}
	members, err := h.svc.List(r.Context(), ac.OrgID)
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"members": members})
}

func (h *Handler) updateRole(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	// Early action-level gate; the service makes the definitive decision once
	// the target's current role is known (owner-protection guard).
	if !authz.Can(ac, authz.ActionMemberRole, &authz.Resource{OrgID: ac.OrgID}) {
		shared.WriteErr(w, shared.Forbidden())
		return
	}
	targetUser, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		shared.WriteErr(w, shared.NotFound())
		return
	}
	var body struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		shared.WriteErr(w, shared.Validation("Invalid JSON body."))
		return
	}
	member, err := h.svc.UpdateRole(r.Context(), ac, targetUser, authz.Role(body.Role))
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"member": member})
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	if !authz.Can(ac, authz.ActionMemberRemove, &authz.Resource{OrgID: ac.OrgID}) {
		shared.WriteErr(w, shared.Forbidden())
		return
	}
	targetUser, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		shared.WriteErr(w, shared.NotFound())
		return
	}
	if err := h.svc.Remove(r.Context(), ac, targetUser); err != nil {
		shared.WriteErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
