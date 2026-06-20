# StatusFlow — Multi-Tenant Status Page SaaS

> A portfolio-grade, security-first multi-tenant SaaS. The product (uptime monitoring + public status pages) is a vehicle; the hireable substance lives in the **tenancy and access-control layer**.

This set of design documents describes a small but production-shaped system whose headline claim is one thing: **cross-tenant data leaks are structurally impossible**, proven by a test suite that actively tries to break isolation and fails.

**Stack:** Go backend (`pgx` + `sqlc`, two binaries: API + worker) · PostgreSQL with Row-Level Security · Next.js (App Router) + shadcn/ui frontend.

---

## The one-sentence thesis

Every tenant-owned row carries an `org_id`; every query is scoped through it in the application layer; and Postgres **Row-Level Security (RLS)** sits underneath as a backstop so the database itself refuses to return another org's rows even when a human forgets a `WHERE`. App-layer scoping is the first line of defense, RLS is the seatbelt, and an automated cross-tenant test suite is the proof.

---

## Document index

| # | Document | What it covers |
|---|----------|----------------|
| 00 | [Overview](./00-overview.md) | Problem, goals, non-goals, the "done properly" definition, success criteria |
| 01 | [High-Level Design (HLD)](./01-hld.md) | System context, components, request lifecycle, tenancy & isolation model, deployment (Go + Next.js) |
| 02 | [Low-Level Design (LLD)](./02-lld.md) | Go package layout, session layer, `WithOrgTx`, `Can()` engine, worker internals, Next.js frontend |
| 03 | [Database Schema](./03-database-schema.md) | Tables, columns, indexes, the **RLS policies**, migration order |
| 04 | [API Schema](./04-api-schema.md) | REST endpoints, request/response shapes, error contract, status codes |
| 05 | [Roles & Authorization](./05-roles-and-authorization.md) | The four roles, the full permission matrix, `Can()` in Go |
| 06 | [Tech Stack & Design Decisions](./06-tech-stack-and-decisions.md) | Stack choices, ADRs (incl. Go, tx-scoped RLS, Next.js/shadcn), trade-offs |
| 07 | [Testing & Security Hardening](./07-testing-and-security.md) | Cross-tenant isolation tests (Go), authz tests, threat model, SSRF, hardening |

> Docs 03 (schema) and 04 (API) are language-agnostic and apply unchanged to the Go implementation — the schema is pure SQL/RLS and the API is a REST contract.

---

## Build path (each step ships something real)

1. **Auth foundation** — signup, login, logout, argon2id passwords, DB-backed sessions. One user, no orgs yet.
2. **Tenancy** — orgs, memberships, `org_id` scoping on the first resource via `WithOrgTx`. **Turn on RLS here, early.**
3. **Roles** — the four roles and the centralized `Can()` check; gate actions by role.
4. **Invitations** — invite by email, accept, join an org with an assigned role.
5. **The product** — monitors, the Go worker that pings on a schedule, incidents, public status page (SSR in Next.js).
6. **Hardening** — audit log, auth rate limiting, session revocation, SSRF guard.
7. **The proof** — cross-tenant isolation tests and authorization tests. The case study is built around this.

---

## How to write this up (so it gets you hired)

Do **not** title the case study *"I built a status page."* Title it around the security property:

> *How I made cross-tenant data leaks structurally impossible — RLS policies, a `WithOrgTx` scoping boundary, and a Go test that strips the `WHERE` clause on purpose and proves the leak still can't happen.*

That framing answers the only question a hiring manager actually has about a full-stack engineer: **can I trust this person with production data?**
