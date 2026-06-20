package incidents

import (
	"net/http"

	"github.com/go-chi/chi/v5"

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
	shared.WriteErr(w, shared.NotImplemented("GET /api/orgs/{orgId}/incidents"))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("POST /api/orgs/{orgId}/incidents"))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("GET /api/orgs/{orgId}/incidents/{id}"))
}

func (h *Handler) addUpdate(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("POST /api/orgs/{orgId}/incidents/{id}/updates"))
}

func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("POST /api/orgs/{orgId}/incidents/{id}/resolve"))
}
