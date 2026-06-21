package monitors

import (
	"encoding/json"
	"net/http"
	"strconv"

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

// Mount mounts org-scoped monitor routes.
// Caller must apply Authn + Tenant beforehand.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/monitors", h.list)
	r.Post("/monitors", h.create)
	r.Get("/monitors/{id}", h.get)
	r.Patch("/monitors/{id}", h.update)
	r.Delete("/monitors/{id}", h.delete)
	r.Get("/monitors/{id}/checks", h.checks)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	if !authz.Can(ac, authz.ActionMonitorRead, &authz.Resource{OrgID: ac.OrgID}) {
		shared.WriteErr(w, shared.Forbidden())
		return
	}
	monitors, err := h.svc.List(r.Context(), ac.OrgID)
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"monitors": monitors})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	var body struct {
		Name            string `json:"name"`
		URL             string `json:"url"`
		Method          string `json:"method"`
		ExpectedStatus  *int   `json:"expectedStatus"`
		IntervalSeconds *int   `json:"intervalSeconds"`
		TimeoutMs       *int   `json:"timeoutMs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		shared.WriteErr(w, shared.Validation("Invalid JSON body."))
		return
	}
	monitor, err := h.svc.Create(r.Context(), ac, CreateInput{
		Name: body.Name, URL: body.URL, Method: body.Method,
		ExpectedStatus: body.ExpectedStatus, IntervalSeconds: body.IntervalSeconds, TimeoutMs: body.TimeoutMs,
	})
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusCreated, map[string]any{"monitor": monitor})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	if !authz.Can(ac, authz.ActionMonitorRead, &authz.Resource{OrgID: ac.OrgID}) {
		shared.WriteErr(w, shared.Forbidden())
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteErr(w, shared.NotFound())
		return
	}
	monitor, err := h.svc.Get(r.Context(), ac.OrgID, id)
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"monitor": monitor})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteErr(w, shared.NotFound())
		return
	}
	var body struct {
		Name            *string `json:"name"`
		URL             *string `json:"url"`
		Method          *string `json:"method"`
		ExpectedStatus  *int    `json:"expectedStatus"`
		IntervalSeconds *int    `json:"intervalSeconds"`
		TimeoutMs       *int    `json:"timeoutMs"`
		IsPaused        *bool   `json:"isPaused"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		shared.WriteErr(w, shared.Validation("Invalid JSON body."))
		return
	}
	monitor, err := h.svc.Update(r.Context(), ac, id, UpdateInput{
		Name: body.Name, URL: body.URL, Method: body.Method,
		ExpectedStatus: body.ExpectedStatus, IntervalSeconds: body.IntervalSeconds,
		TimeoutMs: body.TimeoutMs, IsPaused: body.IsPaused,
	})
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"monitor": monitor})
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteErr(w, shared.NotFound())
		return
	}
	if err := h.svc.Delete(r.Context(), ac, id); err != nil {
		shared.WriteErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) checks(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	if !authz.Can(ac, authz.ActionMonitorRead, &authz.Resource{OrgID: ac.OrgID}) {
		shared.WriteErr(w, shared.Forbidden())
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteErr(w, shared.NotFound())
		return
	}
	limit := 50
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil {
			limit = n
		}
	}
	checks, err := h.svc.Checks(r.Context(), ac.OrgID, id, limit)
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"checks": checks})
}
