# 02 — Low-Level Design (LLD)

Below the component boxes of the HLD: Go package layout, function signatures, and the exact mechanics of the auth, tenancy, authorization, and worker subsystems. Code is illustrative Go (`pgx` v5 + `sqlc`); the *shapes* matter more than any single line.

**Backend:** Go. **Frontend:** Next.js (App Router) + shadcn/ui — see §12.

## 1. Project layout (Go)

```
cmd/
  api/main.go            # API server entrypoint
  worker/main.go         # background worker entrypoint
internal/
  http/
    server.go            # router (chi), plugins, error writer
    middleware/
      requestctx.go      # request id, logger
      authn.go           # cookie -> session -> user (401)
      tenant.go          # org -> membership -> role in context (404)
      ratelimit.go       # auth-endpoint throttling
  auth/
    password.go          # argon2id (no hand-rolled crypto)
    session.go           # create / resolve / revoke sessions
    tokens.go            # CSPRNG token gen + sha256 hashing
  authz/
    can.go               # the single Can(ctx, action, resource)
    policy.go            # role -> permission matrix (data)
  tenancy/
    context.go           # WithOrgTx — the isolation linchpin
    membership.go        # resolve/verify membership
  domain/
    orgs/ memberships/ invitations/ monitors/ incidents/ statuspages/ audit/
      handler.go         # HTTP handlers
      service.go         # business logic; opens WithOrgTx
  db/
    pool.go              # pgxpool as the RESTRICTED app_user role
    migrations/          # SQL migrations incl. RLS policies
    queries/             # *.sql for sqlc
    sqlc/                # generated type-safe query code
  worker/
    scheduler.go         # ticker loop + goroutine pool
    claim.go             # SKIP LOCKED claim of due monitors
    pinger.go            # HTTP check w/ timeout + SSRF guard
    incidents.go         # open/resolve incident state machine
  shared/
    errors.go            # AppError + 401/403/404 helpers
    config.go            # env parsing/validation (fail-fast at boot)
test/
  isolation/             # cross-tenant attack suite (the proof)
  authz/                 # permission-matrix table tests
```

> Two binaries (`cmd/api`, `cmd/worker`), one shared `internal/`. Both connect to the same Postgres as the restricted `app_user` role.

## 2. The session layer (owned, but no hand-rolled crypto)

### 2.1 Principles
- The cookie stores **only an opaque random token**, never user data or a self-signed JWT.
- The server stores a **hash** of the token in a `sessions` row, so a DB leak doesn't hand out live sessions.
- Sessions are **server-side and revocable** — the whole point of owning the layer.

### 2.2 Token generation (`auth/tokens.go`)
```go
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// NewSessionToken returns 32 bytes of CSPRNG entropy, URL-safe.
// This raw value goes in the cookie and is never stored.
func NewSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken stores only the hash of the cookie token in the DB.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
```

### 2.3 Password hashing (`auth/password.go`)
Delegate **all** crypto to a vetted implementation. Two acceptable options:

```go
// Pragmatic: a thin, well-regarded wrapper over x/crypto/argon2.
import "github.com/alexedwards/argon2id"

func HashPassword(pw string) (string, error) {
	return argon2id.CreateHash(pw, argon2id.DefaultParams) // argon2id, PHC-encoded
}

func VerifyPassword(encodedHash, pw string) (bool, error) {
	return argon2id.ComparePasswordAndHash(pw, encodedHash) // constant-time
}
```

If you prefer zero extra deps, call `golang.org/x/crypto/argon2`'s `argon2.IDKey` directly and PHC-encode the salt+params+key yourself. Either way the rule holds: **a vetted primitive, argon2id preferred; never invent your own.** bcrypt (`golang.org/x/crypto/bcrypt`) is an acceptable substitute.

### 2.4 Session lifecycle (`auth/session.go`)
```go
type SessionStore struct{ pool *pgxpool.Pool }

// Create inserts {token_hash, user_id, expires_at} and returns the RAW token for the cookie.
func (s *SessionStore) Create(ctx context.Context, userID uuid.UUID) (rawToken string, err error)

// Resolve hashes the cookie token, looks it up, checks not expired/revoked, returns the user.
func (s *SessionStore) Resolve(ctx context.Context, rawToken string) (*User, error)

func (s *SessionStore) Revoke(ctx context.Context, sessionID uuid.UUID) error      // logout
func (s *SessionStore) RevokeAllForUser(ctx context.Context, userID uuid.UUID) error // logout-everywhere
```
> `sessions` is a global (non-tenant) table, so these queries run on the pool directly — no org context needed.

### 2.5 Cookie attributes
```go
http.SetCookie(w, &http.Cookie{
	Name: "sf_session", Value: rawToken, Path: "/",
	HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	MaxAge: int(ttl.Seconds()),
})
```
`HttpOnly` keeps JS from reading it; `Secure` forces HTTPS; `SameSite=Lax` blunts CSRF on top-level navigations. State-changing endpoints additionally require a CSRF token or an `Origin`/`Sec-Fetch-Site` check.

## 3. Auth middleware (`http/middleware/authn.go`)
```go
type ctxKey string
const userKey ctxKey = "user"

func Authn(sessions *auth.SessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie("sf_session")
			if err != nil {
				WriteErr(w, Unauthorized()); return // 401
			}
			user, err := sessions.Resolve(r.Context(), c.Value)
			if err != nil || user == nil {
				WriteErr(w, Unauthorized()); return // 401
			}
			ctx := context.WithValue(r.Context(), userKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
```

## 4. Tenancy & isolation (the linchpin)

### 4.1 Design choice: short transactions, explicit `tx` threading
In Go we do **not** hold a database transaction open across HTTP response serialization. Instead:

1. **Tenant middleware** resolves the user's membership for the route's org (its own brief tx), and stashes `{orgID, role}` in the request context. No membership → `404`.
2. **The service layer** opens `WithOrgTx(orgID)` around the actual unit of DB work and threads the resulting `pgx.Tx` into the repositories.

This keeps transactions short, makes the isolation boundary **visible in the code** (you can see exactly where the org is pinned and which `tx` carries it), and plays nicely with PgBouncer transaction pooling. It's the Go-idiomatic answer to what `AsyncLocalStorage` does in Node — but explicit instead of magic.

### 4.2 The transaction wrapper (`tenancy/context.go`)
```go
// WithOrgTx opens a tx, pins the active org as a TRANSACTION-LOCAL setting,
// then runs fn with that tx. RLS reads app.current_org_id from this connection.
func WithOrgTx(ctx context.Context, pool *pgxpool.Pool, orgID uuid.UUID,
	fn func(tx pgx.Tx) error) error {

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // no-op after a successful Commit

	// set_config(key, value, is_local=true) => cleared at COMMIT/ROLLBACK,
	// cannot leak across pooled connections.
	if _, err := tx.Exec(ctx,
		"SELECT set_config('app.current_org_id', $1, true)", orgID.String()); err != nil {
		return err
	}

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
```

### 4.3 Membership resolution (`tenancy/membership.go`)
The membership check itself runs inside `WithOrgTx`, so RLS scopes the `memberships` table to the requested org. If a row for this user exists in that org, membership is confirmed and we learn the role; if not, the query returns nothing → `404`.

```go
func ResolveMembership(ctx context.Context, pool *pgxpool.Pool,
	userID, orgID uuid.UUID) (*Membership, error) {

	var m *Membership
	err := WithOrgTx(ctx, pool, orgID, func(tx pgx.Tx) error {
		// RLS already constrains memberships to org_id = current_org().
		row := tx.QueryRow(ctx,
			`SELECT role FROM memberships WHERE user_id = $1`, userID)
		var role string
		if err := row.Scan(&role); err != nil {
			return err // pgx.ErrNoRows => not a member
		}
		m = &Membership{OrgID: orgID, UserID: userID, Role: Role(role)}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil // caller turns this into 404
	}
	return m, err
}
```

### 4.4 Tenant middleware (`http/middleware/tenant.go`)
```go
func Tenant(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := r.Context().Value(userKey).(*auth.User)
			orgID, err := uuid.Parse(chi.URLParam(r, "orgId"))
			if err != nil { WriteErr(w, NotFound()); return }

			m, err := tenancy.ResolveMembership(r.Context(), pool, user.ID, orgID)
			if err != nil { WriteErr(w, Internal(err)); return }
			if m == nil { WriteErr(w, NotFound()); return } // 404, not 403 — hide existence

			ac := authz.AuthContext{UserID: user.ID, OrgID: orgID, Role: m.Role}
			ctx := context.WithValue(r.Context(), authCtxKey, ac)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
```

> Optimization note: membership resolution and the operation are two short transactions. In hot paths you may fuse them by having the handler open a single `WithOrgTx` that first confirms membership, then does the work. Two short txns is fine at this scale and keeps middleware simple.

### 4.5 The three isolation layers (unchanged from HLD)
```
Layer 1  Application scoping   every query carries WHERE org_id = $1
Layer 2  Tenant-context txn    set_config('app.current_org_id', orgID, true) per unit of work
Layer 3  Row-Level Security    DB policy: org_id = current_setting('app.current_org_id')
```
The app connects as `app_user` (`NOSUPERUSER`, no `BYPASSRLS`), so Layer 3 always engages — even if Layer 1 is forgotten.

## 5. The authorization engine (`authz/`)

### 5.1 Vocabulary (`authz/policy.go`)
```go
type Role string
const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
)

type Action string
const (
	ActionMonitorCreate Action = "monitor:create"
	ActionMonitorDelete Action = "monitor:delete"
	ActionMemberInvite  Action = "member:invite"
	ActionMemberRemove  Action = "member:remove"
	ActionMemberRole    Action = "member:role:update"
	ActionOrgDelete     Action = "org:delete"
	ActionAuditRead     Action = "audit:read"
	// …full list in doc 05
)

// The matrix AS DATA — adding an action without an entry denies by default.
var rolePermissions = map[Role]map[Action]bool{
	RoleOwner:  allActions(),
	RoleAdmin:  set(ActionMonitorCreate, ActionMonitorDelete, ActionMemberInvite,
		ActionMemberRemove, ActionMemberRole, ActionAuditRead /* … */),
	RoleMember: set(ActionMonitorCreate /* … no delete, no member mgmt … */),
	RoleViewer: set( /* read-only actions only */ ),
}
```

### 5.2 The single entry point (`authz/can.go`)
```go
type AuthContext struct {
	UserID uuid.UUID
	OrgID  uuid.UUID
	Role   Role
}

type Resource struct {
	OrgID      uuid.UUID
	TargetRole Role // for member-management actions
}

func Can(ac AuthContext, action Action, res *Resource) bool {
	// 1. matrix check (default-deny)
	if !rolePermissions[ac.Role][action] {
		return false
	}
	// 2. cross-org guard — belt to RLS's suspenders
	if res != nil && res.OrgID != uuid.Nil && res.OrgID != ac.OrgID {
		return false
	}
	// 3. structural guards
	switch action {
	case ActionOrgDelete:
		return ac.Role == RoleOwner
	case ActionMemberRemove, ActionMemberRole:
		if res != nil && res.TargetRole == RoleOwner && ac.Role != RoleOwner {
			return false // only the owner may touch the owner (ownership transfer)
		}
	}
	return true
}
```

### 5.3 Usage — one shape, everywhere
```go
if !authz.Can(ac, authz.ActionMonitorDelete, &authz.Resource{OrgID: monitor.OrgID}) {
	WriteErr(w, Forbidden()); return // 403
}
```
No handler ever writes `if role == RoleAdmin`. A CI `grep`/`go vet`-style lint forbids inline role comparisons outside `authz/`.

## 6. Data access with sqlc + tx

Queries are hand-written SQL (`db/queries/*.sql`); `sqlc` generates type-safe Go. Every query keeps `WHERE org_id = $1` **even though RLS also enforces it** — belt and suspenders.

```sql
-- db/queries/monitors.sql
-- name: ListMonitors :many
SELECT * FROM monitors WHERE org_id = $1 ORDER BY created_at;

-- name: InsertMonitor :one
INSERT INTO monitors (org_id, name, url, method, expected_status, interval_seconds, timeout_ms)
VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING *;
```

```go
// internal/domain/monitors/service.go
func (s *Service) List(ctx context.Context, ac authz.AuthContext) ([]sqlc.Monitor, error) {
	if !authz.Can(ac, authz.ActionMonitorRead, nil) {
		return nil, Forbidden()
	}
	var out []sqlc.Monitor
	err := tenancy.WithOrgTx(ctx, s.pool, ac.OrgID, func(tx pgx.Tx) error {
		q := sqlc.New(tx)                       // sqlc queries bound to THIS tx
		rows, err := q.ListMonitors(ctx, ac.OrgID)
		out = rows
		return err
	})
	return out, err
}
```
> The isolation suite deliberately calls a query variant **without** `WHERE org_id` and asserts the database *still* blocks cross-tenant rows via RLS.

## 7. Invitations subsystem
```
Invite(orgID, email, role)     -> insert invitation {token_hash, expires_at}; email a link
Accept(rawToken, user)         -> validate token -> insert membership(user, org, role)
                                  -> mark invitation accepted (idempotent)
```
- Token is CSPRNG; only its hash is stored.
- Accepting requires the invitee to be authenticated first, binding the membership to a real user id.
- Inviting requires `Can(ac, ActionMemberInvite, …)` (owner/admin).
- A unique constraint on `(org_id, user_id)` makes double-accept a `409`/no-op, not two memberships.
- `Accept` is **global** (the user isn't a member yet), so it sets the org context from the invitation's `org_id` inside its own `WithOrgTx`.

## 8. Worker internals (`internal/worker/`)

### 8.1 Scheduler loop with a bounded goroutine pool (`scheduler.go`)
```go
func (w *Worker) Run(ctx context.Context) error {
	t := time.NewTicker(w.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			monitors, err := w.claimDue(ctx, w.batch)
			if err != nil { w.log.Error("claim", "err", err); continue }

			sem := make(chan struct{}, w.concurrency) // bound parallelism
			var wg sync.WaitGroup
			for _, m := range monitors {
				wg.Add(1); sem <- struct{}{}
				go func(m Monitor) {
					defer wg.Done(); defer func() { <-sem }()
					w.runCheck(ctx, m)
				}(m)
			}
			wg.Wait()
		}
	}
}
```
This is where Go shines: many concurrent outbound checks with trivial, idiomatic concurrency control.

### 8.2 Claiming due monitors (`claim.go`)
```go
const claimSQL = `
SELECT id, org_id, url, method, timeout_ms, interval_seconds, expected_status
FROM monitors
WHERE is_paused = false AND next_check_at <= now()
ORDER BY next_check_at
LIMIT $1
FOR UPDATE SKIP LOCKED`
```
`FOR UPDATE SKIP LOCKED` means N worker instances never claim the same monitor — horizontal scaling is safe with only Postgres, no broker.

### 8.3 Running a check with an SSRF guard (`pinger.go`)
```go
// A dialer that REFUSES private / loopback / link-local / metadata IPs.
var safeDialer = &net.Dialer{
	Control: func(network, address string, c syscall.RawConn) error {
		host, _, _ := net.SplitHostPort(address)
		ip := net.ParseIP(host)
		if ip == nil || ip.IsLoopback() || ip.IsPrivate() ||
			ip.IsLinkLocalUnicast() || ip.Equal(net.ParseIP("169.254.169.254")) {
			return errors.New("blocked destination")
		}
		return nil
	},
}
var client = &http.Client{Transport: &http.Transport{DialContext: safeDialer.DialContext}}

func (w *Worker) runCheck(ctx context.Context, m Monitor) {
	cctx, cancel := context.WithTimeout(ctx, time.Duration(m.TimeoutMs)*time.Millisecond)
	defer cancel()

	start := time.Now()
	status, code, errStr := "up", 0, ""
	req, _ := http.NewRequestWithContext(cctx, m.Method, m.URL, nil)
	if res, err := client.Do(req); err != nil {
		status, errStr = "down", err.Error()
	} else {
		code = res.StatusCode
		res.Body.Close()
		if code != m.ExpectedStatus {
			status = "down"
		}
	}
	latency := int(time.Since(start).Milliseconds())

	_ = tenancy.WithOrgTx(ctx, w.pool, m.OrgID, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		_ = q.InsertCheck(ctx, sqlc.InsertCheckParams{ /* … */ })
		_ = q.ScheduleNext(ctx, m.ID, m.IntervalSeconds)
		return w.incidents.Apply(ctx, tx, m.OrgID, m.ID, status)
	})
}
```
The worker pins `app.current_org_id` per monitor's org, so its writes obey the same RLS as the API. The SSRF guard is product-specific hardening (the worker fetches user-supplied URLs).

### 8.4 Incident engine (`incidents.go`)
```
Per monitor:
  UP   --(failure streak >= OPEN_THRESHOLD)----> DOWN  [open incident if none open]
  DOWN --(success streak >= RESOLVE_THRESHOLD)-> UP    [resolve the open incident]

OPEN_THRESHOLD    = 2   (avoid flapping on a single blip)
RESOLVE_THRESHOLD = 2
```
The partial unique index `incidents_one_open_per_monitor` guarantees at most one open incident per monitor even under concurrency; the worker reads the streak from recent `check_results`.

## 9. Error model (`shared/errors.go`)

| Helper | Status | When |
|--------|--------|------|
| `Unauthorized()` | 401 | No/invalid session |
| `Forbidden()` | 403 | Authenticated + member, but `Can()` denied |
| `NotFound()` | 404 | No membership for org, or resource not in active org (hides existence) |
| `Validation()` | 422 | Bad input shape |
| `RateLimited()` | 429 | Auth throttle tripped |
| `Conflict()` | 409 | Idempotency / unique violation |

```go
type AppError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
func (e AppError) Error() string { return e.Code }
func Forbidden() AppError { return AppError{403, "forbidden", "You do not have permission to perform this action."} }
```
> 403 vs 404 is deliberate: **no membership** → `404` (don't confirm the org exists); **member but insufficient role** → `403`.

## 10. Request validation & config
- Validate input at the edge (`go-playground/validator` or hand-written) → `422` on failure.
- `shared/config.go` parses and validates env at boot via a typed struct; the process refuses to start if a secret is missing.

## 11. Concurrency & idempotency notes
- Session resolution is read-only and concurrency-safe.
- Invitation acceptance relies on the `(org_id, user_id)` unique constraint → double-accept yields `409`/no-op.
- Incident open/resolve are guarded by the partial unique index plus `FOR UPDATE` on the monitor row inside the worker tx.

## 12. Frontend (Next.js App Router + shadcn/ui)

The frontend is a thin client over the Go API. It is **not** where business rules or authorization live — every rule is enforced server-side by `Can()` and RLS. Next.js only renders and orchestrates.

```
web/
  app/
    (dashboard)/                 # authenticated app — org-scoped UI
      orgs/[orgId]/monitors/     # client components calling the API
      orgs/[orgId]/incidents/
      orgs/[orgId]/settings/     # members, roles, status pages
    status/[slug]/page.tsx       # PUBLIC status page — server-rendered (SSR/ISR)
    api/                         # OPTIONAL BFF route handlers (see ADR-09 in doc 06)
  components/ui/                 # shadcn/ui components (owned, copied in)
  lib/api.ts                     # typed fetch client
```

Key frontend design points (rationale lives in doc 06, ADR-09):
- **Public status page** (`/status/[slug]`) is a **server component**, rendered with SSR or ISR. It calls the Go public endpoint and is SEO-friendly and cacheable — a natural Next.js win, and it never touches the session.
- **Auth-sensitive calls** go through a thin Next.js **BFF** (route handlers under `app/api/`) so the browser only talks to the Next.js origin and the `sf_session` cookie stays first-party and `HttpOnly`. (Alternatively, deploy the Go API and Next app under the same registrable domain and call the API directly — see ADR-09.)
- **shadcn/ui** components are copied into `components/ui/` rather than imported from a package — you own and can edit them. This mirrors the project's whole ethos: own your layer. They're built on Radix primitives + Tailwind.
- **Server state** via TanStack Query (or the framework's `fetch` caching); forms validated client-side for UX, but the server re-validates and is the only authority.
- The UI **renders the role** it's told about (to hide buttons a viewer can't use), but never *enforces* it — enforcement is `Can()` on the server. Hidden buttons are convenience, not security.
