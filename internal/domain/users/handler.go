// Package users hosts the HTTP handlers for /api/auth/* (signup, login,
// logout, me, session management). Authentication primitives (password,
// session, tokens) live in internal/auth; this package wires them to HTTP.
package users

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ishaangarg9/statusflow/internal/auth"
	"github.com/ishaangarg9/statusflow/internal/http/middleware"
	"github.com/ishaangarg9/statusflow/internal/shared"
)

type Handler struct {
	svc      *Service
	sessions *auth.SessionStore
}

func NewHandler(svc *Service, sessions *auth.SessionStore) *Handler {
	return &Handler{svc: svc, sessions: sessions}
}

// MountPublic mounts unauthenticated auth routes (signup, login).
// Wrap with a rate-limit middleware at the caller.
func (h *Handler) MountPublic(r chi.Router) {
	r.Post("/signup", h.signup)
	r.Post("/login", h.login)
}

// MountAuthenticated mounts session-required auth routes.
// Caller must apply the Authn middleware first.
func (h *Handler) MountAuthenticated(r chi.Router) {
	r.Post("/logout", h.logout)
	r.Post("/logout-all", h.logoutAll)
	r.Get("/me", h.me)
	r.Get("/sessions", h.listSessions)
	r.Delete("/sessions/{id}", h.revokeSession)
}

func (h *Handler) signup(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("POST /api/auth/signup"))
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("POST /api/auth/login"))
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	// Once implemented: revoke the session row for the current cookie, then ClearCookie.
	_ = middleware.SessionFrom(r.Context())
	shared.WriteErr(w, shared.NotImplemented("POST /api/auth/logout"))
}

func (h *Handler) logoutAll(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("POST /api/auth/logout-all"))
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("GET /api/auth/me"))
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("GET /api/auth/sessions"))
}

func (h *Handler) revokeSession(w http.ResponseWriter, r *http.Request) {
	shared.WriteErr(w, shared.NotImplemented("DELETE /api/auth/sessions/{id}"))
}
