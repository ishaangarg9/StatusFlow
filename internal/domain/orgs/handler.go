package orgs

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ishaangarg9/statusflow/internal/authz"
	"github.com/ishaangarg9/statusflow/internal/http/middleware"
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
	user := middleware.UserFrom(r.Context())
	if user == nil {
		shared.WriteErr(w, shared.Unauthorized())
		return
	}
	var body struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		shared.WriteErr(w, shared.Validation("Invalid JSON body."))
		return
	}
	org, err := h.svc.Create(r.Context(), user.ID, CreateInput{Name: body.Name, Slug: body.Slug})
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusCreated, map[string]any{"org": org})
}

func (h *Handler) read(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	if !authz.Can(ac, authz.ActionOrgRead, &authz.Resource{OrgID: ac.OrgID}) {
		shared.WriteErr(w, shared.Forbidden())
		return
	}
	org, err := h.svc.Read(r.Context(), ac.OrgID)
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	org.Role = ac.Role
	shared.WriteJSON(w, http.StatusOK, map[string]any{"org": org})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	if !authz.Can(ac, authz.ActionOrgUpdate, &authz.Resource{OrgID: ac.OrgID}) {
		shared.WriteErr(w, shared.Forbidden())
		return
	}
	var body struct {
		Name *string `json:"name"`
		Slug *string `json:"slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		shared.WriteErr(w, shared.Validation("Invalid JSON body."))
		return
	}
	org, err := h.svc.Update(r.Context(), ac.UserID, ac.OrgID, UpdateInput{Name: body.Name, Slug: body.Slug})
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	org.Role = ac.Role
	shared.WriteJSON(w, http.StatusOK, map[string]any{"org": org})
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	if !authz.Can(ac, authz.ActionOrgDelete, &authz.Resource{OrgID: ac.OrgID}) {
		shared.WriteErr(w, shared.Forbidden())
		return
	}
	if err := h.svc.Delete(r.Context(), ac.OrgID); err != nil {
		shared.WriteErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
