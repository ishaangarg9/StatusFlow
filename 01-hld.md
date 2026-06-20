# 01 — High-Level Design (HLD)

**Backend:** Go (two binaries — API + worker). **Frontend:** Next.js (App Router) + shadcn/ui. **Database:** PostgreSQL with Row-Level Security.

## 1. System context

```
   Authenticated users        ┌──────────────────────────────┐
   (owners/admins/...)        │   Next.js app (App Router)   │
        ───────────────────►  │  dashboard UI · shadcn/ui    │
                              │  + BFF route handlers (auth) │
                              └───────────────┬──────────────┘
                                              │ HTTPS (sf_session cookie)
   Public visitors                            ▼
        ──────►  /status/[slug]   ┌───────────────────────────────┐
        (SSR server component)    │      Go API server            │
                          ─────►  │  authn · tenant · Can() · CRUD │
                                  └───┬───────────────┬────────────┘
                                      │ WithOrgTx     │ enqueue/read
                            set_config │               │
                          current_org  ▼               ▼
                            ┌───────────────┐   ┌──────────────┐
                            │  PostgreSQL   │   │ Go worker(s) │
                            │   + RLS       │◄──│ ticker pool +│
                            │  (shared DB)  │   │ HTTP pinger  │
                            └───────────────┘   └──────┬───────┘
                                                       │ outbound checks (SSRF-guarded)
                                                       ▼
                                               monitored endpoints
```

Runtime processes:

1. **Go API server** (`cmd/api`) — handles all authenticated traffic and serves the public status-page JSON. Owns the auth layer, tenant-context, and the `Can()` engine.
2. **Go worker** (`cmd/worker`) — a ticker loop with a bounded goroutine pool; claims due monitors, performs SSRF-guarded HTTP checks, records results, opens/resolves incidents.
3. **PostgreSQL** — single source of truth; RLS is the last line of tenant isolation.
4. **Next.js app** — thin client over the API; renders the dashboard with shadcn/ui and server-renders the public status page.

## 2. Core components

### 2.1 Go API server
- **Router/middleware** (`chi`): request id + structured logging (`slog`), `authn` (cookie → session → user), `tenant` (org → membership → role in context), auth-endpoint rate limiting.
- **Authorization** (`Can()`): one function consulted before any state change or protected read.
- **Domain services/handlers**: orgs, memberships, invitations, monitors, incidents, status pages, audit — each opening `WithOrgTx` for its DB work.
- **Public handler**: a separate, unauthenticated path serving only published status-page data.

### 2.2 Go worker
- **Scheduler**: `time.Ticker` loop; selects monitors with `next_check_at <= now()`.
- **Claim**: `SELECT … FOR UPDATE SKIP LOCKED` so multiple instances never double-ping.
- **Pinger**: `http.Client` with a per-check `context.WithTimeout` and a dial-time SSRF guard.
- **Incident engine**: consecutive-failure/success thresholds open and resolve incidents.

### 2.3 Database
- One shared managed Postgres (Supabase/Neon). Tenant-owned tables carry `org_id` and are protected by RLS. Global tables (`users`, `sessions`) are not tenant-scoped. The app connects as the restricted `app_user` role.

### 2.4 Next.js frontend
- **Dashboard** (authenticated): client components calling the API for org-scoped resources.
- **Public status page** (`/status/[slug]`): a **server component** rendered SSR/ISR — SEO-friendly, cacheable, session-free.
- **BFF route handlers** (optional, `app/api/`): proxy auth-sensitive calls so the browser only talks to the Next origin and the session cookie stays first-party + `HttpOnly` (see doc 06, ADR-11).
- **shadcn/ui**: components copied into the repo (Radix + Tailwind), owned and editable.

## 3. The tenancy & isolation model (the heart of the system)

Three layers; a breach requires all three to fail at once.

```
  Layer 1  Application scoping     every query includes WHERE org_id = $1
  Layer 2  Tenant-context txn      set_config('app.current_org_id', org, true) per unit of work
  Layer 3  Row-Level Security      DB policy: org_id = current_setting('app.current_org_id')
```

- **Layer 1 (first line):** services and `sqlc` queries always filter by the active org.
- **Layer 2 (context):** each unit of DB work runs inside a `WithOrgTx` that first sets the transaction-local org variable. Membership is verified *before* the variable is set.
- **Layer 3 (backstop):** RLS policies compare each row's `org_id` against `current_setting('app.current_org_id')`. The app connects as a non-superuser without `BYPASSRLS`, so a query that forgets its `WHERE` still returns only the active org's rows.

> Key property: forgetting the app-layer filter is a latent bug, not a breach. The database still refuses other orgs' rows. The test suite verifies this deliberately.

### 3.1 How the active org is chosen
- The org is part of the route (`/api/orgs/:orgId/...`).
- Tenant middleware resolves the `membership` for `(user, org)` inside a `WithOrgTx(org)` — RLS scopes `memberships` to that org, so a returned row both confirms membership and yields the role. No row → `404` (not `403`; we don't confirm the org exists).
- The membership's role is attached to the request context for `Can()`.

## 4. Request lifecycle (authenticated)

```
1.  Browser → (optional BFF) → Go API with sf_session cookie.
2.  authn middleware: cookie → session → user.            (else 401)
3.  tenant middleware: route org → membership(user, org)  (else 404);
       role stored in request context.
4.  Handler calls Can(ac, action, resource).              (else 403)
5.  Service opens WithOrgTx(org): set_config(current_org); runs sqlc queries on that tx.
6.  Data-access layer queries WITH org scoping; RLS independently enforces org.
7.  Audit-log row written for state-changing actions.
8.  Commit; serialize JSON response.
```
Transactions are short and never held open across response serialization.

## 5. Request lifecycle (public status page)

```
1.  Next.js server component requests GET /api/public/status/:slug (no cookie).
2.  Go API opens a read-only WithOrgTx for the page's org — resolved from the slug
    ONLY if the status page is public.
3.  Returns a strict projection: page title, listed monitors' up/down state, open/recent incidents.
4.  Never exposes internal ids, member data, monitored URLs, or unpublished monitors.
5.  Next.js renders (SSR/ISR) and caches.
```

## 6. Data flow: a monitor check

```
worker tick ─► claim due monitors (SKIP LOCKED, one tx)
            ─► goroutine pool: for each monitor, HTTP check with timeout + SSRF guard
            ─► WithOrgTx(monitor.org): insert check_result; schedule next; run incident engine
            ─► incident engine:
                 N consecutive failures  ─► open incident (partial-unique index ⇒ at most one open)
                 M consecutive successes ─► resolve open incident
```
The worker pins `app.current_org_id` per monitor's org, so its writes obey the same RLS as the API.

## 7. Deployment topology

| Concern | Choice |
|---------|--------|
| API server | Stateless Go binary (`cmd/api`); scale horizontally behind a load balancer |
| Worker | 1+ stateless Go binaries (`cmd/worker`); `SKIP LOCKED` makes N instances safe |
| Frontend | Next.js on Vercel or a container; public status page via SSR/ISR |
| Database | Managed Postgres (Supabase/Neon); RLS enabled; app connects as `app_user` |
| Migrations | Run by a separate privileged role (not `app_user`) via `golang-migrate`/`goose` |
| Sessions | Server-side rows in Postgres (revocable); cookie holds only an opaque token |
| Rate-limit state | In-process (`x/time/rate`) or Redis |
| Secrets | Platform secret store / env; never in the repo |
| TLS | Terminated at the load balancer / platform edge |

## 8. Cross-cutting concerns
- **Observability:** structured `slog` JSON with request id and active `org_id`; never log secrets or full tokens.
- **Idempotency:** invitation acceptance and incident transitions are safe to repeat.
- **Failure isolation:** every outbound check has a hard `context` timeout so one slow endpoint can't stall the worker.
- **Least privilege:** `app_user` cannot bypass RLS and cannot run DDL.

## 9. What a reviewer should take away

A single shared database that *cannot* leak across tenants because of RLS; a per-unit-of-work tenant transaction with the org pinned visibly in Go code; one centralized authorization function; a separate hardened public surface server-rendered by Next.js; and a concurrency-clean Go worker. "Multi-tenancy done properly," visible at a glance.
