# 06 — Tech Stack & Design Decisions

What the stack is and, more importantly, *why* — in ADR (Architecture Decision Record) style, since the reasoning is the part that signals seniority. Every "swappable" note tells you which choices are cosmetic and which are load-bearing.

## 1. The stack at a glance

| Layer | Choice | Load-bearing? |
|-------|--------|---------------|
| Backend language | **Go** | Swappable, but chosen deliberately (ADR-09) |
| HTTP router | `chi` (or `net/http`, Go 1.22+ routing) | Swappable |
| Database | **PostgreSQL** | **Load-bearing** — RLS is a Postgres feature |
| DB driver | **`pgx` v5** (`pgxpool`) | Load-bearing pattern (tx-scoped RLS) |
| Queries | **`sqlc`** — type-safe Go from hand-written SQL | Pattern load-bearing, tool swappable |
| Migrations | `golang-migrate` (or `goose`) | Swappable |
| Password hashing | **argon2id** via `alexedwards/argon2id` or `x/crypto/argon2` | Load-bearing principle |
| Tokens | `crypto/rand` + `crypto/sha256` | Load-bearing principle |
| Sessions | **Server-side, DB-backed, opaque token** | Load-bearing principle |
| Worker | Goroutine pool + `SELECT … FOR UPDATE SKIP LOCKED` | Pattern load-bearing |
| Rate-limit store | `x/time/rate` in-process, or Redis | Swappable |
| Validation | `go-playground/validator` (or hand-written) | Swappable |
| Frontend | **Next.js (App Router) + TypeScript + Tailwind + shadcn/ui** | Swappable, deliberately thin |
| Frontend data | `fetch` / TanStack Query; public page via server components | Swappable |
| DB hosting | Supabase or Neon (managed Postgres) | Swappable |

> Reuse the same managed Postgres (Supabase/Neon) you already set up. You need Postgres anyway, and RLS lives there.

## 2. Decisions (ADR-style)

### ADR-01 — PostgreSQL with Row-Level Security for isolation
**Decision:** Single shared Postgres; tenant isolation enforced by `org_id` scoping **and** RLS.
**Why:** RLS turns "forgot a `WHERE`" from a data breach into a latent bug. It is the single most credible signal of "multi-tenancy done properly," and almost nobody implements it.
**Alternatives rejected:** app-layer scoping only (no backstop); database-per-tenant (operationally heavy, hard to demo); schema-per-tenant (migration/connection complexity without the clean policy story).
**Consequence:** The app must connect as a non-superuser role without `BYPASSRLS`, and each unit of work must set `app.current_org_id` in a transaction.

### ADR-02 — Own the session layer, but never hand-roll crypto
**Decision:** Build signup/login/logout, server-side revocable sessions, and cookie handling ourselves; delegate all crypto to vetted libraries (argon2id for passwords, `crypto/rand` for tokens).
**Why:** Owning sessions demonstrates real understanding (revocation, expiry, httpOnly cookies). Rolling your own password hashing is the one place "I did it myself" reads as a **red flag**.
**Consequence:** Store only token *hashes*; a DB leak doesn't yield live sessions.

### ADR-03 — Opaque DB-backed sessions over self-signed JWTs
**Decision:** Cookie carries a random opaque token; the server resolves it to a `sessions` row.
**Why:** Instant, true revocation (`logout-everywhere`, kill a stolen session) — impossible with stateless JWTs without extra denylist machinery. Revocability is precisely what owning the session layer buys you.
**Trade-off:** A DB lookup per request; negligible at this scale.

### ADR-04 — Centralized `Can(user, action, resource)` for authorization
**Decision:** One function for every permission decision; the role→action matrix is data; no inline role checks.
**Why:** Centralization is a maturity signal and makes authorization exhaustively testable. Scattered `if role == "admin"` checks rot and drift.
**Consequence:** A CI lint/grep forbids `Role` comparisons outside `authz/`. Every gated mutation writes an audit row.

### ADR-05 — RBAC, not ABAC
**Decision:** Four fixed roles. **Why:** Sufficient for the domain; attribute rules add complexity with no payoff. **Future:** `Can()` is the seam to introduce ABAC later without touching call sites.

### ADR-06 — Background worker with `SELECT … FOR UPDATE SKIP LOCKED`
**Decision:** A separate Go process claims due monitors with `SKIP LOCKED`; a bounded goroutine pool runs the checks.
**Why:** N worker instances never double-ping, using only Postgres — no broker. Go's goroutines make many concurrent outbound checks idiomatic and cheap.
**Alternative considered:** a dedicated queue (e.g. River/asynq + Redis). Fine, but adds infra; `SKIP LOCKED` is enough and shows a sharp Postgres technique.

### ADR-07 — Public status page as a separate, hardened, read-only surface
**Decision:** Public pages resolve their org from a slug (only if `is_public`), run read-only, and return a strict minimal projection.
**Why:** This is the clean public-vs-private boundary the project shows off. Keeping it separate prevents leaking internal fields (monitored URLs, member data, audit logs).

### ADR-08 — 404 vs 403 discipline
**Decision:** No membership for an org → `404`; member but insufficient role → `403`.
**Why:** A `403` for an org you can't access would *confirm it exists*. `404` hides existence.

### ADR-09 — Go for the backend
**Decision:** Implement the API and worker in Go (`pgx` + `sqlc`), not an ORM-driven stack.
**Why:**
- The worker is concurrency-heavy (many simultaneous outbound HTTP checks). Goroutines + a bounded pool make this idiomatic and fast — Go's natural strength.
- Two small static binaries (`api`, `worker`) deploy trivially.
- `pgx` + `sqlc` keep us **close to hand-written SQL**, which is exactly where RLS lives. We are not fighting an ORM's query generation.
- Explicit `tx` threading (ADR-10) makes the tenant-isolation boundary **visible in the code** — a feature for a project whose entire pitch is isolation.
**Trade-off:** More boilerplate than a TS/ORM stack; you write SQL by hand (mitigated by `sqlc` generating the types).
**Rejected:** GORM or any auto-scoping ORM — it would hide the very boundary we want to showcase, and its implicit query building works against the RLS-first design.

### ADR-10 — Transaction-scoped RLS context in Go
**Decision:** Per unit of work: `pool.Begin` → `set_config('app.current_org_id', orgID, true)` → run `sqlc` queries on that **same `pgx.Tx`** → `Commit`. Encapsulated in `tenancy.WithOrgTx`.
**Why:** RLS reads the org from a setting that must live on the *same connection* as the queries. `set_config(..., true)` is transaction-local, so it can't leak across pooled connections and is compatible with PgBouncer transaction pooling. We thread `tx` explicitly rather than using goroutine-local magic — explicit is safer and reviewable.
**Consequence:** Repos take a `pgx.Tx`/`sqlc.Queries`; we never run an org-scoped query on the bare pool. We also do **not** hold a tx open across HTTP response serialization (transactions stay short).

### ADR-11 — Frontend: Next.js (App Router) + shadcn/ui
**Decision:** Thin Next.js client over the Go API; shadcn/ui for components.
**Why:**
- The **public status page** (`/status/[slug]`) becomes a **server-rendered** route (SSR/ISR) — SEO-friendly, cacheable, and it never touches the session. A clean fit for Next.js.
- **shadcn/ui** components are *copied into the repo* (built on Radix + Tailwind), not pulled from a black-box package — you own and can edit them. That mirrors the project's ethos: own your layer.
- Authorization and isolation stay 100% server-side; the UI only renders what it's told and hides controls a `viewer` can't use (convenience, not security).
**Cookie/origin sub-decision:** Prefer **one of**:
1. *BFF proxy* — Next.js route handlers under `app/api/` forward auth-sensitive calls to the Go API, so the browser only talks to the Next origin and `sf_session` stays first-party + `HttpOnly`; **or**
2. *Same registrable domain* — serve Next at `app.example.com` and Go at `api.example.com` with the session cookie scoped to `.example.com`, and call the Go API directly.
Both avoid third-party-cookie and CORS headaches. Pick the BFF if you want the browser to never see the API origin at all.
**Rejected:** putting any authorization logic in the Next.js layer. The frontend is not a trust boundary.

## 3. Environment & configuration

```
DATABASE_URL=postgres://app_user:…@host/db   # the RESTRICTED role, not a superuser
SESSION_TTL_DAYS=14
WORKER_TICK_MS=5000
WORKER_BATCH=50
WORKER_CONCURRENCY=20
INCIDENT_OPEN_THRESHOLD=2
INCIDENT_RESOLVE_THRESHOLD=2
RATE_LIMIT_LOGIN_PER_MIN=10
# Frontend
NEXT_PUBLIC_API_BASE_URL=https://api.example.com   # or the BFF path
```
`internal/shared/config.go` parses these into a typed struct and validates at boot; the process refuses to start if a secret is missing. Secrets never live in the repo.

## 4. Local development

- One `docker-compose` with Postgres; run migrations 001→007 (RLS + grants included).
- A separate **migration role** (privileged) runs DDL; the app/worker connect as `app_user` (DML only, no `BYPASSRLS`).
- Seed two orgs and several users across roles — this seed is what the isolation tests run against.
- Three processes locally: `go run ./cmd/api`, `go run ./cmd/worker`, and `next dev` for the frontend.
- `sqlc generate` regenerates query code from `db/queries/*.sql` after schema changes.

## 5. Non-functional posture (right-sized, not over-built)

| Concern | Stance |
|---------|--------|
| Scalability | Stateless API + worker scale horizontally; `SKIP LOCKED` makes N workers safe |
| Availability | Single managed DB; multi-region out of scope |
| Performance | `org_id` indexes + RLS (sub-ms policy overhead); Go handles the concurrent pinger well |
| Security | Defense-in-depth isolation, owned sessions, rate limiting, audit log, SSRF guard (doc 07) |
| Observability | Structured logs (`slog`) with request id + active org id; never log secrets/tokens |
| Cost | One small Postgres + two small Go processes + a static/SSR Next.js app |

## 6. What's deliberately *not* here

No billing, no websockets, no on-call paging, no per-tenant DBs, no ABAC, no microservices, no ORM. Each omission is a conscious scope decision — saying so in the write-up is itself a seniority signal. The value is concentrated in the tenancy and access layer; everything else is the minimum vehicle needed to exercise it.
