# CLAUDE.md — StatusFlow

Project memory for AI agents and contributors. Read this first, every session. If a request conflicts with the **Non-negotiable invariants** below, stop and flag it rather than complying.

---

## 1. What this project is (and what actually matters)

StatusFlow is a multi-tenant uptime/status-page SaaS. The product is a vehicle; **the hireable substance is the tenancy and access-control layer.** The headline claim the whole repo must defend:

> **Cross-tenant data leaks are structurally impossible** — every tenant row carries `org_id`, every query is org-scoped in the app, and Postgres **Row-Level Security (RLS)** is the backstop that refuses another org's rows even when a `WHERE` is forgotten. A test suite proves it.

When in doubt about a trade-off, optimize for *demonstrable isolation and clarity*, not features. Adding product surface is cheap; weakening the isolation story is not allowed.

**Design docs are the source of truth.** Read the relevant one before implementing:
`00-overview` · `01-hld` · `02-lld` · `03-database-schema` · `04-api-schema` · `05-roles-and-authorization` · `06-tech-stack-and-decisions` · `07-testing-and-security`.

---

## 2. Non-negotiable invariants (NEVER violate these)

These are the spine of the project. A change that breaks one is wrong even if it "works."

1. **The app connects ONLY as `app_user`** — `NOSUPERUSER`, no `BYPASSRLS`, no DDL. DDL/migrations run as a *separate* privileged role. Never widen `app_user`'s privileges to make something work.
2. **Every tenant-owned table has RLS.** `ENABLE` **and** `FORCE ROW LEVEL SECURITY`, plus a policy with both `USING` and `WITH CHECK` on `org_id = current_org()`. No exceptions, no "I'll add it later."
3. **All org-scoped DB work goes through `tenancy.WithOrgTx(ctx, pool, orgID, fn)`.** Never run an org-scoped query on the bare pool — RLS depends on `set_config('app.current_org_id', …, true)` living on the *same* tx as the query.
4. **Belt and suspenders:** org-scoped queries still include `WHERE org_id = $1` even though RLS enforces it. Both layers, always.
5. **All permission decisions go through `authz.Can(ac, action, resource)`.** Never write `if role == …` outside `internal/authz/`. CI greps for this and fails the build.
6. **Crypto is delegated.** Passwords: argon2id only. Tokens: `crypto/rand`, store only the `sha256` hash. Never hand-roll crypto. Never put a self-signed JWT in the cookie.
7. **Sessions are opaque, DB-backed, and revocable.** The cookie holds a random token; the server resolves it to a `sessions` row. Revocation must work (single + all).
8. **404 vs 403 discipline:** no membership for an org → `404` (hide existence); member but insufficient role → `403`. Never leak that an org/resource exists.
9. **The worker's outbound fetches use the SSRF-guarded `http.Client` only.** Never `http.DefaultClient` for monitor checks. The dial-time guard rejects loopback/private/link-local/metadata IPs.
10. **The public status endpoint returns the strict projection only.** Never select internal fields (monitored `url`, member emails, audit logs, unpublished monitors) into a public response.
11. **Every gated mutation writes an `audit_logs` row.**
12. **The frontend is not a trust boundary.** It may hide controls for UX; it must never be the thing that enforces a permission.

If a task seems to require breaking one of these, the task is wrong — surface it.

---

## 3. Tech stack (use these, don't substitute without asking)

| Layer | Choice |
|-------|--------|
| Backend | **Go** — two binaries: `cmd/api`, `cmd/worker` |
| Router | `chi` (or `net/http` 1.22+ routing) |
| DB | **PostgreSQL** (RLS is load-bearing — never swap the DB) |
| Driver | `pgx` v5 (`pgxpool`) |
| Queries | `sqlc` — hand-written SQL in `db/queries/*.sql`, generated Go types |
| Migrations | `golang-migrate` (or `goose`) |
| Passwords | `alexedwards/argon2id` (or `x/crypto/argon2`) |
| Tokens | `crypto/rand` + `crypto/sha256` |
| Rate limit | `x/time/rate` (in-process) or Redis |
| Logging | `slog` (structured JSON) |
| Frontend | **Next.js (App Router) + TypeScript + Tailwind + shadcn/ui** |
| DB host | Supabase / Neon |

**No ORM.** GORM and any auto-scoping ORM are rejected (ADR-09) — they hide the isolation boundary that is the entire point. Stay close to SQL.

Adding any new dependency requires a one-line justification and should prefer the standard library.

---

## 4. Project structure

```
cmd/{api,worker}/main.go     # entrypoints
internal/
  http/{server.go,middleware/}   # authn, tenant, ratelimit
  auth/{password,session,tokens}.go
  authz/{can,policy}.go          # the ONLY place role logic lives
  tenancy/{context,membership}.go # WithOrgTx lives here
  domain/<resource>/{handler,service,repo}.go
  db/{pool.go,migrations/,queries/,sqlc/}
  worker/{scheduler,claim,pinger,incidents}.go
  shared/{errors,config}.go
test/{isolation/,authz/}       # the proof
web/                           # Next.js app
```

**Layering:** handlers are thin (parse → authorize → call service → write response). The `Can()` gate lives in the handler by default, but may live in the **service** when that makes it uniformly unit-testable, or when the decision needs data only the service can read (e.g. a target member's current role) — `memberships` (structural guards) and `invitations` (the `member:invite` gate) decide in the service. Either way the decision goes through `authz.Can`; the rule is *where* the call sits, not whether it happens. Services own the `WithOrgTx` boundary and business logic. Repos take a `pgx.Tx` and do data access only. Never call the DB from a handler directly; never put auth logic in a repo.

---

## 5. The recipe: adding a tenant-owned table/resource

Follow every step — skipping one is how isolation bugs are born.

1. **Migration:** add the table with `org_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE`, plus `CREATE INDEX … ON <t>(org_id)`.
2. **RLS:** in the RLS migration, `ENABLE` + `FORCE ROW LEVEL SECURITY` and a `<t>_isolation` policy: `USING (org_id = current_org()) WITH CHECK (org_id = current_org())`.
3. **Grants:** ensure `app_user` has DML (it inherits the schema grant; verify no special-casing needed).
4. **Queries:** write `db/queries/<t>.sql` with `WHERE org_id = $1` on every read/update/delete; run `sqlc generate`.
5. **Service:** wrap all DB work in `tenancy.WithOrgTx`; gate the operation with `authz.Can`.
6. **Action + matrix:** add the `resource:verb` action(s) to `authz/policy.go`; default-deny means unlisted = denied.
7. **API:** nest the route under `/api/orgs/:orgId/…`; map errors to the right status code.
8. **Audit:** write an `audit_logs` row on every mutation.
9. **Tests:** add the resource to the **isolation suite** (Org A cannot touch Org B's rows) and add **authz matrix** cases. *Not done without these.*

---

## 6. Coding conventions (Go)

- `gofmt`/`goimports` clean; `go vet` and `staticcheck` must pass; `sqlc generate` output must be committed and not stale.
- `context.Context` is the first parameter of anything that does I/O; thread it through.
- Errors: wrap with `%w`; return `shared.AppError` for anything that maps to an HTTP status. **Never `panic` in request paths.** Map a `WithOrgTx` closure's error with `shared.MapAppErr` (passes intended statuses through, else 500) — don't blanket-wrap as `Internal`.
- Reuse the shared primitives instead of re-implementing them per domain: `shared.ValidEmail`, `authz.ValidRole` / `authz.AssignableViaInvite` (role identity lives in `authz`), `shared.MapAppErr`, `shared.IsUniqueViolation`.
- Never edit files under `internal/db/sqlc/` by hand — they are generated.
- No global mutable state beyond config and the pool wired at boot.
- Validate input at the edge → `422`; don't trust the client.
- **Never log** session tokens, password hashes, raw passwords, or invite tokens. Logs carry request id + active `org_id` and nothing sensitive.

## 7. Conventions (frontend)

- Enforcement is server-side; the UI only renders the role it's told and hides controls a `viewer` can't use (convenience, not security).
- The public status page (`/status/[slug]`) is a **server component** (SSR/ISR) and must never send the session cookie.
- Auth-sensitive calls go through the Next.js BFF route handlers (or same-registrable-domain setup) so `sf_session` stays first-party + `HttpOnly` — see ADR-11. Don't expose the API origin to the browser if using the BFF.
- shadcn/ui components are owned in `web/components/ui/` — edit them freely.

---

## 8. Definition of done (per feature)

- [ ] RLS policy present on any new tenant table (`ENABLE` + `FORCE` + `USING`/`WITH CHECK`).
- [ ] All DB access via `WithOrgTx`; queries carry `WHERE org_id`.
- [ ] Permission routed through `Can()`; action in the matrix; no inline role check.
- [ ] Mutations write an audit row.
- [ ] Isolation test added (cross-tenant attempt → 403/404/empty, never another org's data).
- [ ] Authz matrix cases added where roles differ.
- [ ] `go vet` + `staticcheck` + `sqlc generate` (no drift) clean; tests green against real Postgres.

---

## 9. Build path (current phase first)

1. ~~**Auth foundation** — signup/login/logout, argon2id, DB-backed sessions.~~ **DONE.**
2. ~~**Tenancy** — orgs, memberships, first resource via `WithOrgTx`. RLS on.~~ **DONE.**
   Orgs CRUD (create enrolls caller as owner), member list/role-update/remove with
   atomic ownership transfer + owner-protection, audit read+write, `/me` cross-org
   listing via the `user_memberships()` SECURITY DEFINER fn (migration 008), and the
   cross-tenant isolation proof in `test/isolation/`. RLS was already on from migration 006.
3. ~~**Roles** — the four roles + `Can()`; gate actions.~~ **DONE.**
   `authz.Can` + the doc-05 matrix + structural guards (owner-only delete, ownership
   transfer, owner-protection, cross-org) are wired into every real org-scoped handler
   (orgs/memberships/audit). Enforcement is proven by the CI gate (`.github/workflows/ci.yml`):
   an exhaustive permission-matrix test (fails on any `policy.go` drift; a new action
   without a matrix row fails too) and `TestNoInlineRoleChecks` (fails the build if a
   `Role` comparison appears outside `internal/authz/`). Remaining product resources get
   gated as they're built in Phase 5.
4. ~~**Invitations** — invite by email, accept, join with a role.~~ **DONE.**
   Org-scoped issue/list/revoke and the global `POST /api/invitations/accept`. The
   `member:invite` gate (owner/admin) lives in the **service** for all three management
   ops (so it is unit-tested and uniform; handlers are thin). Resend via upsert (preserves
   the original `created_at`); owner-role invites refused; existing-member refused; `List`
   returns only live (unaccepted, unexpired) invites; `Revoke` only deletes pending invites
   so an accepted invitation's row is never destroyed.
   Accept is the one pre-membership write: the org is found via the locked-down
   `invitation_org_by_token()` SECURITY DEFINER fn (migration 010; its NULL result is
   scanned into a `*uuid.UUID` so an unknown token is an explicit nil → 422), then the
   join + single-use mark + audit run inside `WithOrgTx` with RLS armed. Invites are CSPRNG
   tokens stored sha256-hashed, bound to the target email, single-use, and 7-day expiring.
   The raw token leaves the server ONLY through the `invitations.Mailer` interface (dev
   impl `OutboxMailer` writes to a filesystem outbox; a real transport is Phase 6) — it is
   never returned over the API or logged.
   Proven by `test/invitations/` (happy path, email-binding 403, expiry/unknown/used 422,
   already-member/double-accept 409, resend invalidation, revoke incl. accepted-invite
   protection, and the member:invite role gate on create/list/revoke) and an invitation
   cross-tenant case in `test/isolation/`.
5. ~~**Product** — monitors, the worker (`SKIP LOCKED`), incidents, public status page.~~ **DONE.**
   Monitors (CRUD + `/checks`, gated `monitor:*`), incidents (manual open/update/resolve,
   gated `incident:*`, with the partial unique index turning a double-open into 409), and
   status pages (admin CRUD + `PUT …/monitors`, gated `statuspage:publish`/`statuspage:read`).
   The **public status page** (`GET /api/public/status/{slug}`) is unauthenticated: the org is
   resolved by the locked-down `public_status_page_org()` SECURITY DEFINER fn (migration 011;
   returns a row ONLY when `is_public`, NULL→404 hides existence), then the strict projection
   is read inside `WithOrgTx` under RLS — never URLs, emails, audit, or unpublished data.
   The **worker** claims due monitors via the `claim_due_monitors()` SECURITY DEFINER fn
   (migration 012) that atomically locks a batch with `FOR UPDATE SKIP LOCKED` **and** bumps
   `next_check_at`, so N instances never double-process; each check is persisted and the
   incident engine (open after `OPEN_THRESHOLD` consecutive downs, resolve after
   `RESOLVE_THRESHOLD` ups; system-authored audit rows) runs inside the per-monitor
   `WithOrgTx`. The four SECURITY DEFINER fns (`user_memberships`, `invitation_org_by_token`,
   `public_status_page_org`, `claim_due_monitors`) are the only sanctioned RLS escape hatches;
   each returns minimal data and is EXECUTE-granted only to `app_user`. Proven by `test/product/`
   (monitor lifecycle, URL validation 422, incident double-open 409, public projection hides
   URL + respects visibility, incident engine open/resolve) and product cross-tenant cases in
   `test/isolation/` (check_results/incidents/status_pages invisibility + the public fn).
   **start at Phase 6 next** (note: the invitation-delivery outbox table + worker drain,
   deferred from Phase 4, is a natural tie-in to fold into Phase 6 hardening).
6. **Hardening** — audit log, auth rate limiting, session revocation, SSRF guard.
7. **The proof** — cross-tenant isolation tests + authz tests. The case study is built on this.

> Update this section as phases complete. Keep it honest about where the project actually is.

---

## 10. Commands

```bash
# DB (local)
docker compose up -d db
migrate -path internal/db/migrations -database "$DATABASE_URL" up   # runs as the PRIVILEGED role

# Codegen
sqlc generate

# Run
go run ./cmd/api
go run ./cmd/worker
cd web && pnpm dev

# Quality gates
go vet ./... && staticcheck ./...
go test ./...
go test ./test/isolation/... ./test/authz/...   # the proof; needs real Postgres
```

`DATABASE_URL` for the app uses the **restricted `app_user`** role. Migrations use a separate privileged role. Never point the app at a superuser connection.

---

## 11. Ask before doing (don't decide these silently)

- Changing the auth/session model (e.g. introducing JWTs) or the cookie/origin strategy.
- Adding a new dependency, especially an ORM or anything that touches DB access.
- Weakening, deferring, or "temporarily" disabling any invariant in §2 (including RLS during local dev).
- Putting any authorization or isolation logic in the frontend.
- Broadening `app_user` privileges or the public status-page projection.

When unsure, prefer the more isolated, more explicit, more testable option — and leave a comment explaining why.
