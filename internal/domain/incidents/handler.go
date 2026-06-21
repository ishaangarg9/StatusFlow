package incidents

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

func (h *Handler) Mount(r chi.Router) {
	r.Get("/incidents", h.list)
	r.Post("/incidents", h.create)
	r.Get("/incidents/{id}", h.get)
	r.Post("/incidents/{id}/updates", h.addUpdate)
	r.Post("/incidents/{id}/resolve", h.resolve)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	if !authz.Can(ac, authz.ActionIncidentRead, &authz.Resource{OrgID: ac.OrgID}) {
		shared.WriteErr(w, shared.Forbidden())
		return
	}
	incs, err := h.svc.List(r.Context(), ac.OrgID)
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"incidents": incs})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	var body struct {
		MonitorID string `json:"monitorId"`
		Title     string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		shared.WriteErr(w, shared.Validation("Invalid JSON body."))
		return
	}
	monitorID, err := uuid.Parse(body.MonitorID)
	if err != nil {
		shared.WriteErr(w, shared.Validation("A valid monitorId is required."))
		return
	}
	inc, err := h.svc.Create(r.Context(), ac, CreateInput{MonitorID: monitorID, Title: body.Title})
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusCreated, map[string]any{"incident": inc})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	if !authz.Can(ac, authz.ActionIncidentRead, &authz.Resource{OrgID: ac.OrgID}) {
		shared.WriteErr(w, shared.Forbidden())
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteErr(w, shared.NotFound())
		return
	}
	inc, err := h.svc.Get(r.Context(), ac.OrgID, id)
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"incident": inc})
}

func (h *Handler) addUpdate(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteErr(w, shared.NotFound())
		return
	}
	var body struct {
		Message string `json:"message"`
		Status  string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		shared.WriteErr(w, shared.Validation("Invalid JSON body."))
		return
	}
	inc, err := h.svc.AddUpdate(r.Context(), ac, id, UpdateInput{Message: body.Message, Status: body.Status})
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusCreated, map[string]any{"incident": inc})
}

func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteErr(w, shared.NotFound())
		return
	}
	inc, err := h.svc.Resolve(r.Context(), ac, id)
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"incident": inc})
}
