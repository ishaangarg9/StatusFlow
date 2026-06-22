# StatusFlow

A multi-tenant uptime-monitoring and status-page SaaS, written in Go and PostgreSQL with a Next.js frontend. Organizations create monitors, a background worker pings them on a schedule, incidents open and resolve automatically, and each org can publish a public status page.

The product is real, but the point of the project is the **tenancy and access-control layer**. Its headline guarantee:

> **Cross-tenant data leaks are structurally impossible.** Every tenant row carries an `org_id`, every query is org-scoped in the application, and PostgreSQL **Row-Level Security (RLS)** is the backstop that refuses another org's rows even when a `WHERE` clause is forgotten — proven by a test suite that strips the filter on purpose and shows the leak still can't happen.

---

## How isolation is enforced

Four independent layers protect tenant data; a leak would require all of them to fail at once.

1. **Application scoping** — every org-scoped query carries `WHERE org_id = $1`.
2. **Transaction boundary** — all org-scoped work runs inside `tenancy.WithOrgTx(ctx, pool, orgID, fn)`, which sets `app.current_org_id` on the *same* transaction as the query.
3. **RLS backstop** — every tenant table has `ENABLE` + `FORCE ROW LEVEL SECURITY` with a policy `USING (org_id = current_org()) WITH CHECK (org_id = current_org())`.
4. **Least privilege** — the app connects as `app_user` (`NOSUPERUSER`, no `BYPASSRLS`, no DDL), so application code *cannot* turn RLS off. Migrations run as a separate privileged role.

Authorization is centralized: the four roles (owner / admin / member / viewer) and every gated action live in `internal/authz`, and every decision goes through `authz.Can(...)`. A CI gate fails the build if a role comparison appears anywhere outside that package, or if the permission matrix drifts.

---

## Tech stack

| Layer | Choice |
|-------|--------|
| Backend | Go 1.25 — two binaries: `cmd/api`, `cmd/worker` |
| Database | PostgreSQL 16 (RLS is load-bearing) |
| DB driver | `pgx` v5 (`pgxpool`) |
| Queries | `sqlc` — hand-written SQL in `internal/db/queries`, generated Go types |
| Migrations | `golang-migrate` |
| Passwords | argon2id |
| Sessions | opaque, DB-backed, revocable (random token → `sessions` row) |
| Frontend | Next.js (App Router) + TypeScript + Tailwind + shadcn/ui |

No ORM — the code stays close to SQL so the isolation boundary is always visible.

---

## Project structure

```
cmd/{api,worker}/        # the two entrypoints
internal/
  http/                  # router, authn / tenant / rate-limit middleware
  auth/                  # password, session, token primitives
  authz/                 # the ONLY place role logic lives (Can + policy)
  tenancy/               # WithOrgTx and membership resolution
  domain/<resource>/     # handler -> service -> repo per resource
  db/{migrations,queries,sqlc}/   # SQL, generated types, pool
  worker/                # scheduler, claim/lease, pinger (SSRF-guarded), incidents
  shared/                # errors, config
test/{isolation,authz,roles,invitations,product,sessions,audit}/   # the proof
web/                     # Next.js app (app/, components/, lib/)
```

---

## Prerequisites

- **Go** 1.25+
- **Docker** (for the local Postgres) — or your own PostgreSQL 16
- **golang-migrate** CLI — `go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.1`
- **Node 18+** and **pnpm** (or npm) — only for the frontend
- *Optional:* `sqlc` and `staticcheck`, needed only to regenerate queries / lint

---

## Getting started

### 1. Configure environment

```bash
cp .env.example .env
```

The defaults work out of the box with the bundled Docker Postgres. Note the two DSNs:

- `DATABASE_URL` — the app, connecting as the **restricted** `app_user`.
- `MIGRATIONS_DATABASE_URL` — migrations, connecting as the **privileged** `postgres` role.

Never point the app at the privileged role. Local Postgres is exposed on host port **5433** (to avoid clashing with other local instances); the API listens on **:8080**.

### 2. Start the database

```bash
make db.up           # docker compose up -d db
```

The first migration creates the `app_user` and privileged roles inside the database.

### 3. Run migrations (as the privileged role)

```bash
set -a; . ./.env; set +a          # export the env into your shell
make migrate.up                   # migrate ... -database "$MIGRATIONS_DATABASE_URL" up
```

### 4. Run the backend

In separate terminals (both need the env exported):

```bash
make api             # go run ./cmd/api      — serves the REST API on :8080
make worker          # go run ./cmd/worker   — pings monitors, drives incidents + delivery
```

### 5. Run the frontend

```bash
cd web
pnpm install
pnpm dev             # Next.js dev server on :3000, talks to the API at NEXT_PUBLIC_API_BASE_URL
```

Open http://localhost:3000. The public status page lives at `/status/<slug>` and is server-rendered without the session cookie.

---

## Running the tests (the proof)

The DB-backed suites connect as `app_user` against a **real Postgres** with RLS forced on. They **skip** (not fail) when `DATABASE_URL` is unset, so the unit tests stay green without a database.

```bash
set -a; . ./.env; set +a

make test            # go test ./...  — everything
make test.proof      # go test ./test/isolation/... ./test/authz/...  — the isolation + authz proof
```

What the proof covers:

- **Isolation** (`test/isolation/`) — every tenant-owned table has a cross-tenant case proving reads are invisible across orgs (RLS `USING`) and cross-org writes are refused (RLS `WITH CHECK`), including the headline test that removes the app-level `WHERE` and shows the DB still won't leak.
- **Authorization** (`test/authz/`) — an exhaustive, self-policing permission matrix plus a guard that fails the build on any inline role check outside `internal/authz`.
- **Per-domain** (`test/product`, `test/invitations`, `test/sessions`, `test/audit`, and the `internal/worker` / `internal/auth` package tests) — public-projection safety, session revocation, audit pagination/retention, the SSRF dial guard, and worker claim/lease behavior.

CI (`.github/workflows/ci.yml`) stands up `postgres:16`, runs migrations as the privileged role, connects the suite as `app_user`, and runs the authz gate plus the full RLS suite on every push and pull request.

---

## Make targets

| Target | Does |
|--------|------|
| `make db.up` / `make db.down` | start / stop the local Postgres |
| `make migrate.up` / `make migrate.down` | apply / roll back one migration (privileged role) |
| `make sqlc` | regenerate Go types from `internal/db/queries` |
| `make api` / `make worker` | run the two backend binaries |
| `make test` / `make test.proof` | full suite / the isolation + authz proof |
| `make lint` | `go vet` + `staticcheck` |

---

## Configuration reference

All configuration is via environment variables (see `.env.example`):

| Variable | Purpose |
|----------|---------|
| `DATABASE_URL` | app connection (restricted `app_user`) |
| `MIGRATIONS_DATABASE_URL` | migration connection (privileged role) |
| `API_ADDR` | API listen address (default `:8080`) |
| `SESSION_TTL_DAYS`, `SESSION_COOKIE_NAME`, `SESSION_COOKIE_SECURE` | session cookie behavior |
| `WORKER_TICK_MS`, `WORKER_BATCH`, `WORKER_CONCURRENCY` | worker scheduling |
| `WORKER_CLAIM_LEASE_SECONDS` | re-check window for a crashed worker's in-flight monitor |
| `INCIDENT_OPEN_THRESHOLD`, `INCIDENT_RESOLVE_THRESHOLD` | consecutive checks before opening / resolving an incident |
| `RATE_LIMIT_LOGIN_PER_MIN`, `RATE_LIMIT_ACCEPT_PER_MIN` | per-IP rate limits on login and invite-accept |
| `NEXT_PUBLIC_API_BASE_URL` | API origin the frontend calls |
