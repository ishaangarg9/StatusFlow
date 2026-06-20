package monitors

import (
	"net/http"

	"github.com/go-chi/chi/v5"

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

// Reference shape for a fully-wired handler (the recipe from CLAUDE.md §5):
//
//   ac := middleware.AuthContextFrom(r.Context())
//   if !authz.Can(ac, authz.ActionMonitorCreate, &authz.Resource{OrgID: ac.OrgID}) {
//       shared.WriteErr(w, shared.Forbidden()); return
//   }
//   var body createMonitorReq
//   if err := json.NewDecoder(r.Body).Decode(&body); err != nil { ... 422 ... }
//   monitor, err := h.svc.Create(r.Context(), ac, body)
//   if err != nil { shared.WriteErr(w, err); return }
//   shared.WriteJSON(w, http.StatusCreated, monitor)

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("GET /api/orgs/{orgId}/monitors"))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("POST /api/orgs/{orgId}/monitors"))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("GET /api/orgs/{orgId}/monitors/{id}"))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("PATCH /api/orgs/{orgId}/monitors/{id}"))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("DELETE /api/orgs/{orgId}/monitors/{id}"))
}

func (h *Handler) checks(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("GET /api/orgs/{orgId}/monitors/{id}/checks"))
}
