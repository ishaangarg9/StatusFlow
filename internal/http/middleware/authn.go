package middleware

import (
	"context"
	"net/http"

	"github.com/ishaangarg9/statusflow/internal/auth"
	"github.com/ishaangarg9/statusflow/internal/shared"
)

const (
	userKey    ctxKey = "user"
	sessionKey ctxKey = "session"
)

// Authn resolves the session cookie to a (user, session). No cookie or no
// valid session -> 401. The session row is stashed in context so handlers
// like "logout current" can revoke it without re-hashing the cookie.
func Authn(sessions *auth.SessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(sessions.CookieName())
			if err != nil {
				shared.WriteErr(w, shared.Unauthorized())
				return
			}
			user, sess, err := sessions.Resolve(r.Context(), c.Value)
			if err != nil || user == nil {
				shared.WriteErr(w, shared.Unauthorized())
				return
			}
			ctx := context.WithValue(r.Context(), userKey, user)
			ctx = context.WithValue(ctx, sessionKey, sess)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func UserFrom(ctx context.Context) *auth.User {
	if v, ok := ctx.Value(userKey).(*auth.User); ok {
		return v
	}
	return nil
}

func SessionFrom(ctx context.Context) *auth.Session {
	if v, ok := ctx.Value(sessionKey).(*auth.Session); ok {
		return v
	}
	return nil
}
