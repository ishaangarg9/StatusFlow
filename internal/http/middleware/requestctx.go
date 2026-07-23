package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"
)

type ctxKey string

const (
	requestIDKey ctxKey = "request_id"
	loggerKey    ctxKey = "logger"
)

// logHolder is a per-request, mutable carrier for the request-scoped logger.
// It is stored once (as a pointer) in the request context; middleware downstream
// of RequestContext — e.g. Tenant — enriches it via EnrichLogger, and because the
// holder is shared by pointer those fields are visible to every later log line
// for the request, including the "request" completion line emitted below. Only
// ever mutated within a single request's sequential middleware chain.
type logHolder struct {
	logger *slog.Logger
}

// RequestContext attaches a request id and a request-scoped slog.Logger.
// Every log line carries request_id; downstream code adds active org_id etc.
func RequestContext(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid := newRequestID()
			w.Header().Set("X-Request-Id", rid)

			holder := &logHolder{logger: base.With("request_id", rid, "method", r.Method, "path", r.URL.Path)}
			ctx := context.WithValue(r.Context(), requestIDKey, rid)
			ctx = context.WithValue(ctx, loggerKey, holder)

			start := time.Now()
			next.ServeHTTP(w, r.WithContext(ctx))
			holder.logger.Info("request", "dur_ms", time.Since(start).Milliseconds())
		})
	}
}

// EnrichLogger adds structured fields to the request-scoped logger so all
// subsequent lines for this request — including the completion line — carry them.
// No-op if RequestContext is not mounted. Not safe for concurrent use within one
// request; call it from the synchronous middleware chain (e.g. Tenant), not from
// a goroutine the handler spawns. Never pass sensitive values (tokens, hashes).
func EnrichLogger(ctx context.Context, args ...any) {
	if h, ok := ctx.Value(loggerKey).(*logHolder); ok {
		h.logger = h.logger.With(args...)
	}
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func RequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

func Logger(ctx context.Context) *slog.Logger {
	if h, ok := ctx.Value(loggerKey).(*logHolder); ok {
		return h.logger
	}
	return slog.Default()
}
