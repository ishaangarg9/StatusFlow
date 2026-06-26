package shared

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgconn"
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

// PlanLimit is returned when an org has hit a server-enforced entitlement cap
// (e.g. the Free plan's monitor limit). 402 Payment Required so the UI can show
// an "upgrade" CTA distinct from a permission (403) or validation (422) error.
func PlanLimit(msg string) *AppError {
	if msg == "" {
		msg = "Your plan's limit has been reached. Upgrade to add more."
	}
	return &AppError{http.StatusPaymentRequired, "plan_limit", msg}
}
func Internal(err error) *AppError {
	return &AppError{http.StatusInternalServerError, "internal", "Something went wrong."}
}
func NotImplemented(name string) *AppError {
	return &AppError{http.StatusNotImplemented, "not_implemented", name + " is not implemented yet."}
}

// IsUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505). Centralizes the one place that knows how a
// duplicate-key error looks, so callers map it to a 409 consistently.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// MapAppErr passes a *AppError straight through (e.g. a Forbidden/Validation
// raised deep inside a tenancy.WithOrgTx closure) and converts anything else
// into a 500. Services call this on the error returned by WithOrgTx so an
// intended HTTP status is preserved instead of being masked as Internal.
func MapAppErr(err error) error {
	var ae *AppError
	if errors.As(err, &ae) {
		return ae
	}
	return Internal(err)
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
