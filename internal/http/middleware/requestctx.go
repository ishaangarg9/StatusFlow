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

// RequestContext attaches a request id and a request-scoped slog.Logger.
// Every log line carries request_id; downstream code adds active org_id etc.
func RequestContext(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rid := newRequestID()
			w.Header().Set("X-Request-Id", rid)

			log := base.With("request_id", rid, "method", r.Method, "path", r.URL.Path)
			ctx := context.WithValue(r.Context(), requestIDKey, rid)
			ctx = context.WithValue(ctx, loggerKey, log)

			start := time.Now()
			next.ServeHTTP(w, r.WithContext(ctx))
			log.Info("request", "dur_ms", time.Since(start).Milliseconds())
		})
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
	if v, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return v
	}
	return slog.Default()
}
