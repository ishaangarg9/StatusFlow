package billing

import (
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ishaangarg9/statusflow/internal/http/middleware"
	"github.com/ishaangarg9/statusflow/internal/shared"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// MountOrgScoped mounts the authenticated, tenant-scoped billing routes under
// /api/orgs/{orgId}. Caller applies Authn + Tenant beforehand.
func (h *Handler) MountOrgScoped(r chi.Router) {
	r.Get("/billing/subscription", h.subscription)
	r.Post("/billing/checkout", h.checkout)
	r.Post("/billing/portal", h.portal)
}

// MountPublic mounts the unauthenticated Stripe webhook (mounted under
// /api/stripe). It must NOT be behind Authn/Tenant — Stripe calls it directly —
// and its trust comes entirely from the signature check in the service.
func (h *Handler) MountPublic(r chi.Router) {
	r.Post("/webhook", h.webhook)
}

func (h *Handler) subscription(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	view, err := h.svc.Get(r.Context(), ac)
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"subscription": view})
}

func (h *Handler) checkout(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	url, err := h.svc.StartCheckout(r.Context(), ac)
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"url": url})
}

func (h *Handler) portal(w http.ResponseWriter, r *http.Request) {
	ac := middleware.AuthContextFrom(r.Context())
	url, err := h.svc.StartPortal(r.Context(), ac)
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"url": url})
}

// webhook reads the raw body (signature is computed over the exact bytes),
// verifies + applies it. A bad signature is a 400 (don't act, don't retry); a
// transient apply failure is a 500 so Stripe retries; success is 200.
func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if err := h.svc.HandleWebhook(r.Context(), body, r.Header.Get("Stripe-Signature")); err != nil {
		if errors.Is(err, ErrInvalidSignature) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
