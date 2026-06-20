# 03 — Database Schema

PostgreSQL. Tenant-owned tables carry `org_id` and are protected by Row-Level Security. Global tables (`users`, `sessions`) are not tenant-scoped. All SQL below is the source of truth for migrations; apply in the numbered order in §9.

## 1. Conventions

- Primary keys are `uuid` (`gen_random_uuid()` from `pgcrypto`).
- Timestamps are `timestamptz`, default `now()`.
- Every tenant-owned table: a non-null `org_id uuid REFERENCES organizations(id) ON DELETE CASCADE`, plus an index on `org_id`, plus an RLS policy.
- The application connects as role `app_user` — **`NOSUPERUSER`, no `BYPASSRLS`, no DDL** — so RLS is always enforced.

## 2. Extensions & roles

```sql
CREATE EXTENSION IF NOT EXISTS pgcrypto;   -- gen_random_uuid()

-- The restricted role the API/worker connect as.
CREATE ROLE app_user LOGIN PASSWORD '***';
-- Grants are issued per-table below; app_user gets DML only, never DDL.
```

## 3. Global tables (not tenant-scoped)

### users
```sql
CREATE TABLE users (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email         citext UNIQUE NOT NULL,           -- case-insensitive
  password_hash text NOT NULL,                     -- argon2id; never plaintext
  name          text,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
```
> `users` is global because a person may belong to many orgs. It is *not* under RLS; access is mediated only through sessions and never exposed cross-org beyond what a membership reveals.

### sessions
```sql
CREATE TABLE sessions (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash  text NOT NULL UNIQUE,                -- sha256 of the cookie token
  user_agent  text,
  ip          inet,
  created_at  timestamptz NOT NULL DEFAULT now(),
  expires_at  timestamptz NOT NULL,
  revoked_at  timestamptz                          -- non-null => revoked
);
CREATE INDEX sessions_user_idx ON sessions(user_id);
CREATE INDEX sessions_expiry_idx ON sessions(expires_at);
```

## 4. Tenancy tables

### organizations
```sql
CREATE TABLE organizations (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name        text NOT NULL,
  slug        citext UNIQUE NOT NULL,              -- used in URLs
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);
```

### memberships  (user ⟷ org, with a role)
```sql
CREATE TYPE org_role AS ENUM ('owner', 'admin', 'member', 'viewer');

CREATE TABLE memberships (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id      uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role        org_role NOT NULL DEFAULT 'member',
  created_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, user_id)                         -- one membership per user per org
);
CREATE INDEX memberships_org_idx  ON memberships(org_id);
CREATE INDEX memberships_user_idx ON memberships(user_id);
```
> An org should always have exactly one `owner`. Enforce "exactly one owner" in application logic (ownership transfer) rather than a DB constraint, since it spans rows.

### invitations
```sql
CREATE TABLE invitations (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id      uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  email       citext NOT NULL,
  role        org_role NOT NULL DEFAULT 'member',
  token_hash  text NOT NULL UNIQUE,                -- sha256 of the invite token
  invited_by  uuid NOT NULL REFERENCES users(id),
  expires_at  timestamptz NOT NULL,
  accepted_at timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (org_id, email)                           -- one pending invite per email per org
);
CREATE INDEX invitations_org_idx ON invitations(org_id);
```

## 5. Product tables

### monitors
```sql
CREATE TABLE monitors (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id           uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  name             text NOT NULL,
  url              text NOT NULL,
  method           text NOT NULL DEFAULT 'GET',
  expected_status  int  NOT NULL DEFAULT 200,
  interval_seconds int  NOT NULL DEFAULT 60 CHECK (interval_seconds >= 30),
  timeout_ms       int  NOT NULL DEFAULT 10000,
  is_paused        boolean NOT NULL DEFAULT false,
  next_check_at    timestamptz NOT NULL DEFAULT now(),
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX monitors_org_idx        ON monitors(org_id);
CREATE INDEX monitors_due_idx        ON monitors(next_check_at) WHERE is_paused = false;
```

### check_results  (append-only ping history)
```sql
CREATE TABLE check_results (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  monitor_id    uuid NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
  status        text NOT NULL CHECK (status IN ('up','down')),
  status_code   int,
  latency_ms    int,
  error         text,
  checked_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX check_results_monitor_time_idx ON check_results(monitor_id, checked_at DESC);
CREATE INDEX check_results_org_idx          ON check_results(org_id);
```
> `org_id` is denormalized onto `check_results` (and `incident_updates`) so RLS can be applied directly without a join. This is the standard RLS performance pattern.

### incidents
```sql
CREATE TABLE incidents (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id       uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  monitor_id   uuid NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
  status       text NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved')),
  title        text NOT NULL,
  started_at   timestamptz NOT NULL DEFAULT now(),
  resolved_at  timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX incidents_org_idx       ON incidents(org_id);
CREATE INDEX incidents_monitor_idx   ON incidents(monitor_id);
-- At most one OPEN incident per monitor:
CREATE UNIQUE INDEX incidents_one_open_per_monitor
  ON incidents(monitor_id) WHERE status = 'open';
```

### incident_updates  (public timeline)
```sql
CREATE TABLE incident_updates (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id       uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  incident_id  uuid NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
  message      text NOT NULL,
  status       text NOT NULL,            -- e.g. 'investigating','identified','resolved'
  author_id    uuid REFERENCES users(id),-- null => system-generated
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX incident_updates_incident_idx ON incident_updates(incident_id, created_at);
CREATE INDEX incident_updates_org_idx      ON incident_updates(org_id);
```

### status_pages  (public-page config)
```sql
CREATE TABLE status_pages (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id       uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  slug         citext UNIQUE NOT NULL,    -- /status/:slug
  title        text NOT NULL,
  is_public    boolean NOT NULL DEFAULT false,
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX status_pages_org_idx ON status_pages(org_id);

CREATE TABLE status_page_monitors (
  status_page_id uuid NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
  monitor_id     uuid NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
  org_id         uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  PRIMARY KEY (status_page_id, monitor_id)
);
CREATE INDEX status_page_monitors_org_idx ON status_page_monitors(org_id);
```

### audit_logs
```sql
CREATE TABLE audit_logs (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  actor_user_id uuid REFERENCES users(id),     -- null => system/worker
  action        text NOT NULL,                  -- e.g. 'monitor:delete'
  resource_type text,
  resource_id   uuid,
  metadata      jsonb NOT NULL DEFAULT '{}',
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_logs_org_time_idx ON audit_logs(org_id, created_at DESC);
```

## 6. Row-Level Security — the backstop

The pattern is identical for every tenant-owned table. A small helper keeps the policies readable:

```sql
CREATE OR REPLACE FUNCTION current_org() RETURNS uuid
LANGUAGE sql STABLE AS $$
  SELECT NULLIF(current_setting('app.current_org_id', true), '')::uuid
$$;
```

### 6.1 Enable + policy template
```sql
ALTER TABLE monitors ENABLE ROW LEVEL SECURITY;
ALTER TABLE monitors FORCE ROW LEVEL SECURITY;   -- applies even to the table owner

CREATE POLICY monitors_isolation ON monitors
  USING      (org_id = current_org())   -- which rows are visible (SELECT/UPDATE/DELETE)
  WITH CHECK (org_id = current_org());  -- which rows may be written (INSERT/UPDATE)
```

Apply the same two statements to **every** tenant-owned table:

```sql
-- repeat for: memberships, invitations, check_results, incidents,
--             incident_updates, status_pages, status_page_monitors, audit_logs, organizations
ALTER TABLE check_results ENABLE ROW LEVEL SECURITY;
ALTER TABLE check_results FORCE ROW LEVEL SECURITY;
CREATE POLICY check_results_isolation ON check_results
  USING (org_id = current_org()) WITH CHECK (org_id = current_org());
-- ...and so on for each table.
```

> `organizations` itself: the visible-org policy is `id = current_org()`. A user only ever operates inside one active org per request, so they only ever see that org row.

### 6.2 Why this is the seatbelt
- The app connects as `app_user`, which has **no `BYPASSRLS`**, so policies always run.
- `FORCE ROW LEVEL SECURITY` ensures even a table-owner connection is filtered.
- If a developer ships `SELECT * FROM monitors` with no `WHERE org_id`, the policy still restricts results to `current_org()`. A forgotten filter becomes a latent bug, **not a breach**.
- `current_setting('app.current_org_id', true)` is set per request via `SET LOCAL`/`set_config(..., true)` and is transaction-local, so it cannot leak across pooled connections.

### 6.3 Grants (DML only, no DDL)
```sql
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO app_user;
GRANT USAGE ON SCHEMA public TO app_user;
-- No GRANT of CREATE/ALTER/DROP. Migrations run as a separate privileged role.
```

## 7. The public status-page path

The public read uses the same RLS machinery: the request resolves the org from the page `slug` **only if `is_public = true`**, sets `app.current_org_id` to that org for a read-only transaction, and selects a strict projection (page title, listed monitors' up/down state, open/recent incidents and their updates). Internal columns — monitored `url`, member emails, audit logs — are never selected into the public response.

## 8. Entity relationships (text ER)

```
users 1───* memberships *───1 organizations
users 1───* sessions
organizations 1───* invitations
organizations 1───* monitors 1───* check_results
organizations 1───* monitors 1───* incidents 1───* incident_updates
organizations 1───* status_pages *───* monitors   (via status_page_monitors)
organizations 1───* audit_logs
```

## 9. Migration order

1. `001_extensions_and_roles.sql` — pgcrypto, `app_user`, `org_role` enum, `current_org()`.
2. `002_global.sql` — `users`, `sessions`.
3. `003_tenancy.sql` — `organizations`, `memberships`, `invitations`.
4. `004_product.sql` — `monitors`, `check_results`, `incidents`, `incident_updates`, `status_pages`, `status_page_monitors`.
5. `005_audit.sql` — `audit_logs`.
6. `006_rls.sql` — `ENABLE/FORCE ROW LEVEL SECURITY` + isolation policies on every tenant-owned table.
7. `007_grants.sql` — DML grants to `app_user`.

> Turn on RLS (step 6) **early in the build**, while the tenant surface is just `monitors`. Adding RLS table-by-table as you add tables is far easier than retrofitting it later.
