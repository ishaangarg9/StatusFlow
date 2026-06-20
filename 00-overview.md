# 00 — Project Overview

## 1. What this is

**StatusFlow** is a multi-tenant SaaS where organizations register monitors (URLs/endpoints), a background worker pings them on a schedule, failures are recorded as incidents, and each organization gets a shareable **public status page**.

The product is deliberately small and genuinely useful, but it is chosen for its *shape*, not its features:

- It is **more than CRUD** — a background worker runs on a schedule, visibly.
- It has a **clean authorization boundary** — public status pages vs. private admin.
- It forces **real multi-tenancy** — every org's data must be isolated from every other org's.

> The nouns are swappable. Replace "monitors and incidents" with "invoices," "documents," or "feature flags" and every design decision below is identical. The substance lives in the tenancy and access layer.

## 2. Why this project (the strategic point)

Security work is invisible until someone looks for it, and most portfolio projects have nothing there. A reviewer who opens this repo and finds RLS policies, a centralized authorization function, and a passing *"cross-tenant access is impossible"* test immediately knows the author takes isolation seriously rather than hoping an ORM filters correctly. That is a signal most senior engineers cannot produce on demand.

## 3. Goals

1. **Correct multi-tenant isolation** with defense in depth (app-layer scoping **and** database RLS).
2. **An owned session/auth layer** the author fully understands — without hand-rolling crypto.
3. **Centralized, testable authorization** via a single `can(user, action, resource)` function.
4. **A real feature surface** (monitors, worker, incidents, public pages) that exercises tenancy and roles together.
5. **Proof** — an automated test suite that actively attempts cross-tenant and privilege-escalation attacks and verifies they fail.

## 4. Non-goals (explicitly out of scope)

- Attribute-based access control (ABAC). RBAC is sufficient at this scale.
- Hand-rolled password hashing or token crypto. Use vetted libraries (argon2/bcrypt).
- Billing, payments, usage metering.
- Multi-region, sharding, or per-tenant databases. Single shared Postgres with row-level isolation.
- A polished marketing site or heavy frontend. The frontend is a thin client over the API.
- Real-time websockets, on-call paging integrations (PagerDuty, etc.). Mention as "future work."

## 5. The definition of "done properly"

| Naive version | Done properly (this project) |
|---------------|------------------------------|
| `tenant_id` on rows, app filters by it | `tenant_id` on rows **+ RLS backstop** in Postgres |
| Hope every query has a `WHERE org_id` | DB refuses cross-tenant rows even if a `WHERE` is forgotten |
| `if (user.role === 'admin')` scattered in handlers | One centralized `can(user, action, resource)` |
| Roll-your-own password storage | Vetted hashing library + owned session layer |
| "It works when I click around" | Automated tests that *try to break isolation* and fail |
| README: "I built a status page" | Case study: "I made cross-tenant leaks structurally impossible" |

## 6. Success criteria

- [ ] A signed-in member of Org A receives `403` or an empty set on **every** attempt to read or mutate Org B's data — verified by tests.
- [ ] Removing the app-layer `WHERE org_id` from any single query does **not** leak data, because RLS still blocks it — verified by a deliberate test.
- [ ] Every permission decision routes through `can()`; no inline role checks in handlers — verified by lint/grep + tests.
- [ ] Passwords are stored only as argon2/bcrypt hashes; sessions can be revoked; auth endpoints are rate-limited.
- [ ] An audit log records who did what, when, in which org.

## 7. Primary personas

- **Owner** — created the org, full control including deletion and billing-equivalent actions.
- **Admin** — manages monitors, members, and incidents; cannot delete the org or remove the owner.
- **Member** — operates day-to-day: creates/edits monitors, opens/resolves incidents.
- **Viewer** — read-only internal access (e.g., a stakeholder who should see dashboards but change nothing).
- **Public visitor** — unauthenticated; sees only the public status page for an org that has published one.
