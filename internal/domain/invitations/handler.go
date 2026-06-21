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
// Caller must apply Authn + Tenant beforehand.
func (h *Handler) MountOrgScoped(r chi.Router) {
	r.Get("/invitations", h.list)
	r.Post("/invitations", h.create)
	r.Delete("/invitations/{id}", h.revoke)
}

// All three management routes gate on member:invite (owner, admin) — the
// doc-04 access intent for issuing, listing, and revoking invitations. (member:
// read is for the members list; invitations carry pending emails and are
// managed by the same roles that may invite.)
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	if !authz.Can(ac, authz.ActionMemberInvite, &authz.Resource{OrgID: ac.OrgID}) {
		shared.WriteErr(w, shared.Forbidden())
		return
	}
	invs, err := h.svc.List(r.Context(), ac.OrgID)
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"invitations": invs})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	if !authz.Can(ac, authz.ActionMemberInvite, &authz.Resource{OrgID: ac.OrgID}) {
		shared.WriteErr(w, shared.Forbidden())
		return
	}
	var body struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		shared.WriteErr(w, shared.Validation("Invalid JSON body."))
		return
	}
	// The raw token is returned to deliver out-of-band (email). There is no mail
	// transport yet, so it is intentionally dropped here rather than logged or
	// returned to the browser — wiring email delivery is a later hardening step.
	inv, _, err := h.svc.Create(r.Context(), ac, CreateInput{Email: body.Email, Role: authz.Role(body.Role)})
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusCreated, map[string]any{"invitation": inv})
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	if !authz.Can(ac, authz.ActionMemberInvite, &authz.Resource{OrgID: ac.OrgID}) {
		shared.WriteErr(w, shared.Forbidden())
		return
	}
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
