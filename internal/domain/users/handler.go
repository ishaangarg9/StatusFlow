// Package users hosts the HTTP handlers for /api/auth/* (signup, login,
// logout, me, session management). Authentication primitives (password,
// session, tokens) live in internal/auth; this package wires them to HTTP.
package users

import (
	"encoding/json"
	"net"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

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
// Caller wraps with an IP rate-limit middleware.
func (h *Handler) MountPublic(r chi.Router) {
	r.Post("/signup", h.signup)
	r.Post("/login", h.login)
}

// MountAuthenticated mounts session-required auth routes.
// Caller applies the Authn middleware first.
func (h *Handler) MountAuthenticated(r chi.Router) {
	r.Post("/logout", h.logout)
	r.Post("/logout-all", h.logoutAll)
	r.Get("/me", h.me)
	r.Get("/sessions", h.listSessions)
	r.Delete("/sessions/{id}", h.revokeSession)
}

// --- Handlers ------------------------------------------------------------

func (h *Handler) signup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		shared.WriteErr(w, shared.Validation("Invalid JSON body."))
		return
	}
	u, err := h.svc.Signup(r.Context(), SignupInput{
		Email: body.Email, Password: body.Password, Name: body.Name,
	})
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusCreated, map[string]any{"user": u})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		shared.WriteErr(w, shared.Validation("Invalid JSON body."))
		return
	}
	raw, u, err := h.svc.Login(r.Context(), LoginInput{
		Email: body.Email, Password: body.Password,
		UA: r.UserAgent(), IP: remoteIP(r),
	})
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	h.sessions.SetCookie(w, raw)
	shared.WriteJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFrom(r.Context())
	if sess == nil {
		shared.WriteErr(w, shared.Unauthorized())
		return
	}
	if err := h.svc.Logout(r.Context(), sess.ID); err != nil {
		shared.WriteErr(w, err)
		return
	}
	h.sessions.ClearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) logoutAll(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFrom(r.Context())
	if user == nil {
		shared.WriteErr(w, shared.Unauthorized())
		return
	}
	if err := h.svc.LogoutAll(r.Context(), user.ID); err != nil {
		shared.WriteErr(w, err)
		return
	}
	h.sessions.ClearCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFrom(r.Context())
	if user == nil {
		shared.WriteErr(w, shared.Unauthorized())
		return
	}
	u, memberships, err := h.svc.Me(r.Context(), user.ID)
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{
		"user":        u,
		"memberships": memberships,
	})
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFrom(r.Context())
	sess := middleware.SessionFrom(r.Context())
	if user == nil || sess == nil {
		shared.WriteErr(w, shared.Unauthorized())
		return
	}
	list, err := h.svc.ListSessions(r.Context(), user.ID, sess.ID)
	if err != nil {
		shared.WriteErr(w, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"sessions": list})
}

func (h *Handler) revokeSession(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFrom(r.Context())
	if user == nil {
		shared.WriteErr(w, shared.Unauthorized())
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteErr(w, shared.NotFound())
		return
	}
	if err := h.svc.RevokeSession(r.Context(), user.ID, id); err != nil {
		shared.WriteErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// remoteIP returns the connection-level peer IP for session storage.
// Behind a trusted proxy you'd parse X-Forwarded-For instead.
func remoteIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
