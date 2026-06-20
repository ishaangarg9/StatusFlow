# 04 — API Schema

REST over HTTPS, JSON bodies, cookie-based sessions. All authenticated, org-scoped routes are nested under `/api/orgs/:orgId/...` so the tenant context is unambiguous and the tenant middleware always has an org to pin.

## 1. Conventions

- **Auth:** session cookie `sf_session` (httpOnly, Secure, SameSite=Lax). State-changing requests also carry a CSRF token (`X-CSRF-Token`) or pass an Origin/Fetch-Metadata check.
- **Content type:** `application/json` for requests and responses.
- **IDs:** UUID strings.
- **Timestamps:** ISO-8601 UTC.
- **Validation:** request bodies validated at the edge; failures → `422`.

### 1.1 Error envelope
```json
{ "error": { "code": "forbidden", "message": "You do not have permission to perform this action." } }
```

### 1.2 Status codes
| Code | Meaning |
|------|---------|
| 200 / 201 | Success / created |
| 204 | Success, no body (e.g. logout, delete) |
| 401 | No/invalid session |
| 403 | Authenticated + member, but `can()` denied |
| 404 | No membership for org, or resource not in active org (existence hidden) |
| 409 | Conflict (duplicate, double-accept) |
| 422 | Validation error |
| 429 | Rate limited |

## 2. Auth (global, not org-scoped)

| Method | Path | Auth | `can()` | Notes |
|--------|------|------|---------|-------|
| POST | `/api/auth/signup` | public | — | Creates a user. Rate-limited. |
| POST | `/api/auth/login` | public | — | Sets session cookie. Rate-limited. |
| POST | `/api/auth/logout` | session | — | Revokes current session → 204. |
| POST | `/api/auth/logout-all` | session | — | Revokes all sessions for the user. |
| GET | `/api/auth/me` | session | — | Current user + their org memberships. |
| GET | `/api/auth/sessions` | session | — | List active sessions (for revocation UI). |
| DELETE | `/api/auth/sessions/:id` | session | — | Revoke a specific session. |

**POST `/api/auth/signup`**
```jsonc
// request
{ "email": "ada@example.com", "password": "•••••••••", "name": "Ada" }
// 201
{ "user": { "id": "u_…", "email": "ada@example.com", "name": "Ada" } }
```

**POST `/api/auth/login`**
```jsonc
// request
{ "email": "ada@example.com", "password": "•••••••••" }
// 200 — Set-Cookie: sf_session=…; HttpOnly; Secure; SameSite=Lax
{ "user": { "id": "u_…", "email": "ada@example.com" } }
// 401 on bad credentials (generic message — never reveal which field was wrong)
```

## 3. Organizations

| Method | Path | `can()` action | Roles |
|--------|------|----------------|-------|
| POST | `/api/orgs` | — (any authed user) | creator becomes `owner` |
| GET | `/api/orgs/:orgId` | `org:read` | all members |
| PATCH | `/api/orgs/:orgId` | `org:update` | owner, admin |
| DELETE | `/api/orgs/:orgId` | `org:delete` | **owner only** |

**POST `/api/orgs`**
```jsonc
// request
{ "name": "Acme Inc", "slug": "acme" }
// 201 — also creates a memberships row {role:'owner'} for the caller
{ "org": { "id": "o_…", "name": "Acme Inc", "slug": "acme" } }
```

## 4. Members & invitations

| Method | Path | `can()` action | Roles |
|--------|------|----------------|-------|
| GET | `/api/orgs/:orgId/members` | `member:read` | all members |
| PATCH | `/api/orgs/:orgId/members/:userId` | `member:role:update` | owner, admin |
| DELETE | `/api/orgs/:orgId/members/:userId` | `member:remove` | owner, admin |
| POST | `/api/orgs/:orgId/invitations` | `member:invite` | owner, admin |
| GET | `/api/orgs/:orgId/invitations` | `member:read` | owner, admin |
| DELETE | `/api/orgs/:orgId/invitations/:id` | `member:invite` | owner, admin |
| POST | `/api/invitations/accept` | session (any authed user) | invitee |

**POST `/api/orgs/:orgId/invitations`**
```jsonc
// request
{ "email": "grace@example.com", "role": "member" }
// 201 — emails a link containing the raw token; only the hash is stored
{ "invitation": { "id": "inv_…", "email": "grace@example.com", "role": "member", "expiresAt": "…" } }
```

**POST `/api/invitations/accept`** (global; not org-scoped because the user isn't a member yet)
```jsonc
// request — caller must be authenticated (signed up/logged in)
{ "token": "raw-invite-token" }
// 200 — creates membership(user, org, role); idempotent on re-accept
{ "membership": { "orgId": "o_…", "role": "member" } }
// 409 if already a member; 422 if token expired/invalid
```

**PATCH `/api/orgs/:orgId/members/:userId`** (change role)
```jsonc
{ "role": "admin" }   // owner/admin only; cannot demote the sole owner
```

## 5. Monitors

| Method | Path | `can()` action | Roles |
|--------|------|----------------|-------|
| GET | `/api/orgs/:orgId/monitors` | `monitor:read` | all members |
| POST | `/api/orgs/:orgId/monitors` | `monitor:create` | owner, admin, member |
| GET | `/api/orgs/:orgId/monitors/:id` | `monitor:read` | all members |
| PATCH | `/api/orgs/:orgId/monitors/:id` | `monitor:update` | owner, admin, member |
| DELETE | `/api/orgs/:orgId/monitors/:id` | `monitor:delete` | owner, admin |
| GET | `/api/orgs/:orgId/monitors/:id/checks` | `monitor:read` | all members |

**POST `/api/orgs/:orgId/monitors`**
```jsonc
// request
{
  "name": "Marketing site",
  "url": "https://acme.com",
  "method": "GET",
  "expectedStatus": 200,
  "intervalSeconds": 60,
  "timeoutMs": 10000
}
// 201
{
  "monitor": {
    "id": "m_…", "name": "Marketing site", "url": "https://acme.com",
    "isPaused": false, "intervalSeconds": 60, "nextCheckAt": "…", "createdAt": "…"
  }
}
```

**GET `/api/orgs/:orgId/monitors/:id/checks?limit=50`**
```jsonc
// 200
{
  "checks": [
    { "status": "up",   "statusCode": 200, "latencyMs": 142, "checkedAt": "…" },
    { "status": "down", "statusCode": 503, "latencyMs": 87,  "error": "bad status", "checkedAt": "…" }
  ]
}
```

## 6. Incidents

| Method | Path | `can()` action | Roles |
|--------|------|----------------|-------|
| GET | `/api/orgs/:orgId/incidents` | `incident:read` | all members |
| POST | `/api/orgs/:orgId/incidents` | `incident:create` | owner, admin, member |
| GET | `/api/orgs/:orgId/incidents/:id` | `incident:read` | all members |
| POST | `/api/orgs/:orgId/incidents/:id/updates` | `incident:update` | owner, admin, member |
| POST | `/api/orgs/:orgId/incidents/:id/resolve` | `incident:resolve` | owner, admin, member |

**POST `/api/orgs/:orgId/incidents/:id/updates`**
```jsonc
{ "message": "Investigating elevated error rates.", "status": "investigating" }
```

## 7. Status pages (admin side)

| Method | Path | `can()` action | Roles |
|--------|------|----------------|-------|
| GET | `/api/orgs/:orgId/status-pages` | `statuspage:read` | all members |
| POST | `/api/orgs/:orgId/status-pages` | `statuspage:publish` | owner, admin |
| PATCH | `/api/orgs/:orgId/status-pages/:id` | `statuspage:publish` | owner, admin |
| PUT | `/api/orgs/:orgId/status-pages/:id/monitors` | `statuspage:publish` | owner, admin |

**POST `/api/orgs/:orgId/status-pages`**
```jsonc
{ "slug": "acme", "title": "Acme Status", "isPublic": true, "monitorIds": ["m_…", "m_…"] }
```

## 8. Public status page (unauthenticated)

| Method | Path | Auth | Notes |
|--------|------|------|-------|
| GET | `/status/:slug` | **public** | Read-only, strict projection |
| GET | `/api/public/status/:slug` | **public** | JSON for the SPA to render |

**GET `/api/public/status/:slug`**
```jsonc
// 200 — only published data; NO urls, member info, or internal ids beyond what's needed
{
  "title": "Acme Status",
  "overall": "operational",            // derived: operational | degraded | down
  "components": [
    { "name": "Marketing site", "status": "operational" },
    { "name": "API",            "status": "down" }
  ],
  "incidents": [
    {
      "title": "API elevated errors",
      "status": "open",
      "startedAt": "…",
      "updates": [ { "message": "Investigating.", "status": "investigating", "createdAt": "…" } ]
    }
  ]
}
// 404 if slug unknown or page is not public (existence hidden)
```

## 9. Audit log

| Method | Path | `can()` action | Roles |
|--------|------|----------------|-------|
| GET | `/api/orgs/:orgId/audit` | `audit:read` | owner, admin |

```jsonc
// GET /api/orgs/:orgId/audit?limit=50
{
  "entries": [
    { "actor": "ada@example.com", "action": "monitor:delete", "resourceId": "m_…", "createdAt": "…" },
    { "actor": "system",          "action": "incident:open",  "resourceId": "i_…", "createdAt": "…" }
  ]
}
```

## 10. Cross-tenant behavior (the contract the tests enforce)

For **any** org-scoped route, when the caller is a member of Org A and the `:orgId`/resource belongs to Org B:

- If the caller has **no membership** in Org B → `404` (we never confirm Org B exists).
- If a resource id in the path belongs to another org than the active `:orgId` → `404` (RLS makes it invisible; the row simply isn't found in the active org).
- No route, under any role, returns another org's data. This is asserted exhaustively in [07 — Testing & Security](./07-testing-and-security.md).
