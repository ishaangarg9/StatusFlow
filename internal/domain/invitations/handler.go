package invitations

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

// MountGlobal mounts the global invite-accept endpoint.
// Caller must apply Authn beforehand (invitee must be signed in).
func (h *Handler) MountGlobal(r chi.Router) {
	r.Post("/invitations/accept", h.accept)
}

// MountOrgScoped mounts org-scoped invite routes.
// Caller must apply Authn + Tenant beforehand. Authorization (member:invite) is
// enforced in the service so it is unit-tested and uniform across these routes.
func (h *Handler) MountOrgScoped(r chi.Router) {
	r.Get("/invitations", h.list)
	r.Post("/invitations", h.create)
	r.Delete("/invitations/{id}", h.revoke)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	invs, err := h.svc.List(r.Context(), ac)
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"invitations": invs})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	var body struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		shared.WriteErr(w, shared.Validation("Invalid JSON body."))
		return
	}
	inv, err := h.svc.Create(r.Context(), ac, CreateInput{Email: body.Email, Role: authz.Role(body.Role)})
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusCreated, map[string]any{"invitation": inv})
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteErr(w, shared.NotFound())
		return
	}
	if err := h.svc.Revoke(r.Context(), ac, id); err != nil {
		shared.WriteErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// accept is global (not org-scoped): the invitee is authenticated but not yet a
// member, so there is no active org. Authorization is the token itself; no
// authz.Can gate applies.
func (h *Handler) accept(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFrom(r.Context())
	if user == nil {
		shared.WriteErr(w, shared.Unauthorized())
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		shared.WriteErr(w, shared.Validation("Invalid JSON body."))
		return
	}
	res, err := h.svc.Accept(r.Context(), user, body.Token)
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"membership": res})
}
