package middleware

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/authz"
	"github.com/ishaangarg9/statusflow/internal/shared"
	"github.com/ishaangarg9/statusflow/internal/tenancy"
)

const authCtxKey ctxKey = "auth_context"

// Tenant resolves the route's orgId, confirms the active user is a member,
// and stashes an authz.AuthContext in the request. No membership -> 404
// (we don't confirm the org exists). Mount AFTER Authn.
func Tenant(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := UserFrom(r.Context())
			if user == nil {
				shared.WriteErr(w, shared.Unauthorized())
				return
			}
			orgID, err := uuid.Parse(chi.URLParam(r, "orgId"))
			if err != nil {
				shared.WriteErr(w, shared.NotFound())
				return
			}
			m, err := tenancy.ResolveMembership(r.Context(), pool, user.ID, orgID)
			if err != nil {
				shared.WriteErr(w, shared.Internal(err))
				return
			}
			if m == nil {
				shared.WriteErr(w, shared.NotFound()) // 404, not 403 — hide existence
				return
			}

			ac := authz.AuthContext{UserID: user.ID, OrgID: orgID, Role: m.Role}
			ctx := context.WithValue(r.Context(), authCtxKey, ac)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AuthContextFrom returns the (user, org, role) bundle the tenant middleware set.
// Zero value if Tenant was not mounted on this route.
func AuthContextFrom(ctx context.Context) authz.AuthContext {
	if v, ok := ctx.Value(authCtxKey).(authz.AuthContext); ok {
		return v
	}
	return authz.AuthContext{}
}
