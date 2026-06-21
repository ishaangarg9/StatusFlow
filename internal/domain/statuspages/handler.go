package statuspages

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
	ac := middleware.AuthContextFrom(r.Context())
	if !authz.Can(ac, authz.ActionStatusRead, &authz.Resource{OrgID: ac.OrgID}) {
		shared.WriteErr(w, shared.Forbidden())
		return
	}
	pages, err := h.svc.List(r.Context(), ac.OrgID)
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"statusPages": pages})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	var body struct {
		Slug       string   `json:"slug"`
		Title      string   `json:"title"`
		IsPublic   bool     `json:"isPublic"`
		MonitorIDs []string `json:"monitorIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		shared.WriteErr(w, shared.Validation("Invalid JSON body."))
		return
	}
	monitorIDs, err := parseUUIDs(body.MonitorIDs)
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	page, err := h.svc.Create(r.Context(), ac, CreateInput{
		Slug: body.Slug, Title: body.Title, IsPublic: body.IsPublic, MonitorIDs: monitorIDs,
	})
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusCreated, map[string]any{"statusPage": page})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteErr(w, shared.NotFound())
		return
	}
	var body struct {
		Title    *string `json:"title"`
		IsPublic *bool   `json:"isPublic"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		shared.WriteErr(w, shared.Validation("Invalid JSON body."))
		return
	}
	page, err := h.svc.Update(r.Context(), ac, id, UpdateInput{Title: body.Title, IsPublic: body.IsPublic})
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"statusPage": page})
}

func (h *Handler) setMonitors(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteErr(w, shared.NotFound())
		return
	}
	var body struct {
		MonitorIDs []string `json:"monitorIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		shared.WriteErr(w, shared.Validation("Invalid JSON body."))
		return
	}
	monitorIDs, err := parseUUIDs(body.MonitorIDs)
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	page, err := h.svc.SetMonitors(r.Context(), ac, id, monitorIDs)
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"statusPage": page})
}

func (h *Handler) publicView(w http.ResponseWriter, r *http.Request) {
	view, err := h.svc.PublicView(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, view)
}

// parseUUIDs converts a list of string ids to UUIDs, mapping a malformed entry
// to a 422 rather than a generic decode failure.
func parseUUIDs(raw []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, shared.Validation("monitorIds must be valid ids.")
		}
		out = append(out, id)
	}
	return out, nil
}
