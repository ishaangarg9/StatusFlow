package shared

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

type AppError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *AppError) Error() string { return e.Code }

func Unauthorized() *AppError {
	return &AppError{http.StatusUnauthorized, "unauthorized", "Authentication required."}
}
func Forbidden() *AppError {
	return &AppError{http.StatusForbidden, "forbidden", "You do not have permission to perform this action."}
}
func NotFound() *AppError {
	return &AppError{http.StatusNotFound, "not_found", "Not found."}
}
func Validation(msg string) *AppError {
	if msg == "" {
		msg = "Invalid input."
	}
	return &AppError{http.StatusUnprocessableEntity, "validation", msg}
}
func Conflict(msg string) *AppError {
	if msg == "" {
		msg = "Conflict."
	}
	return &AppError{http.StatusConflict, "conflict", msg}
}
func RateLimited() *AppError {
	return &AppError{http.StatusTooManyRequests, "rate_limited", "Too many requests."}
}
func Internal(err error) *AppError {
	return &AppError{http.StatusInternalServerError, "internal", "Something went wrong."}
}
func NotImplemented(name string) *AppError {
	return &AppError{http.StatusNotImplemented, "not_implemented", name + " is not implemented yet."}
}

// WriteErr renders an AppError (or a fallthrough 500) as the standard envelope.
func WriteErr(w http.ResponseWriter, err error) {
	var ae *AppError
	if !errors.As(err, &ae) {
		ae = Internal(err)
	}
	if ae.Status >= 500 {
		slog.Error("internal error", "err", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(ae.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": ae})
}

func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
