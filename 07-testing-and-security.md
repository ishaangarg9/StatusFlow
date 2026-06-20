# 07 — Testing & Security Hardening

The centerpiece of the project. Anyone can *claim* isolation; this document is how you **prove** it. The deliverable a hiring manager remembers is a passing Go test named *"cross-tenant access is impossible."* Go's table-driven testing maps almost one-to-one onto the isolation matrix and the permission matrix, which makes the proof compact and exhaustive.

## 1. The test pyramid for this system

```
            ┌───────────────────────────────┐
            │  Isolation & authz E2E suite  │  ← the proof (most valuable)
            ├───────────────────────────────┤
            │     API integration tests     │  ← real HTTP + real Postgres + RLS
            ├───────────────────────────────┤
            │          unit tests           │  ← Can(), incident state machine, tokens
            └───────────────────────────────┘
```
Integration tests run against **real Postgres with RLS enabled**, connecting as the restricted `app_user`. Use `testcontainers-go` (or a docker-compose test DB) so the database is real — a mock would skip the exact layer being proven. Standard library `testing` + `testify/require` is plenty; no heavy framework needed.

## 2. The cross-tenant isolation suite (`test/isolation/`)

### 2.1 Fixture
```
Org A: owner=alice,  member=bob
Org B: owner=carol,  member=dave
Each org seeded with monitors, incidents, status pages, audit entries.
```

### 2.2 The core assertion, table-driven
> *Signed in as a member of Org A, every attempt to read or mutate Org B's data returns 403/404 or an empty set — never Org B's data.*

```go
func TestCrossTenantAccessIsImpossible(t *testing.T) {
	env := setupTwoOrgs(t)            // real Postgres, RLS on, app_user role
	alice := env.Login(t, "alice")   // member/owner of Org A
	b := env.OrgB

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"list B monitors", "GET", f("/api/orgs/%s/monitors", b.ID)},
		{"read B monitor", "GET", f("/api/orgs/%s/monitors/%s", b.ID, b.MonitorID)},
		{"patch B monitor", "PATCH", f("/api/orgs/%s/monitors/%s", b.ID, b.MonitorID)},
		{"delete B monitor", "DELETE", f("/api/orgs/%s/monitors/%s", b.ID, b.MonitorID)},
		{"list B incidents", "GET", f("/api/orgs/%s/incidents", b.ID)},
		{"list B members", "GET", f("/api/orgs/%s/members", b.ID)},
		{"read B audit", "GET", f("/api/orgs/%s/audit", b.ID)},
		// …one row per org-scoped endpoint
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := alice.Do(t, tc.method, tc.path, nil)
			require.Contains(t, []int{403, 404}, res.Status) // never 200 with B's data
			require.NotContains(t, res.BodyString(), b.SecretMarker)
		})
	}
}
```

### 2.3 The deliberate-bug test (proves RLS is real, not decorative)
The single strongest artifact in the repo: **remove the app-layer filter and show the database still blocks the leak.**

```go
func TestRLSBlocksLeakEvenWithoutAppFilter(t *testing.T) {
	env := setupTwoOrgs(t)

	err := tenancy.WithOrgTx(ctx, env.Pool, env.OrgA.ID, func(tx pgx.Tx) error {
		// A query with NO `WHERE org_id` — simulating a forgotten filter.
		rows, err := tx.Query(ctx, `SELECT org_id FROM monitors`)
		require.NoError(t, err)
		defer rows.Close()

		for rows.Next() {
			var orgID uuid.UUID
			require.NoError(t, rows.Scan(&orgID))
			// RLS constrains visibility to current_org() = Org A.
			require.Equal(t, env.OrgA.ID, orgID)        // every row is Org A
			require.NotEqual(t, env.OrgB.ID, orgID)     // never Org B
		}
		return rows.Err()
	})
	require.NoError(t, err)
}
```
> This demonstrates the seatbelt engages exactly when the first line of defense fails. Lead your case study with this test.

### 2.4 Resource-id confusion test
```go
func TestForeignResourceIdIsNotFound(t *testing.T) {
	env := setupTwoOrgs(t)
	alice := env.Login(t, "alice")
	// Active org = A, but the monitor id belongs to B.
	res := alice.Do(t, "GET", f("/api/orgs/%s/monitors/%s", env.OrgA.ID, env.OrgB.MonitorID), nil)
	require.Equal(t, 404, res.Status) // RLS makes it not-found, never another org's row
}
```

## 3. The authorization suite (`test/authz/`)

Drive the **entire permission matrix** from doc 05 as a table test — Go's idiom *is* the matrix. Adding an action without a `policy.go` entry fails loudly (default-deny).

```go
func TestPermissionMatrix(t *testing.T) {
	cases := []struct {
		role   authz.Role
		action authz.Action
		want   bool
	}{
		{authz.RoleViewer, authz.ActionMonitorCreate, false},
		{authz.RoleMember, authz.ActionMonitorDelete, false},
		{authz.RoleAdmin, authz.ActionOrgDelete, false},
		{authz.RoleOwner, authz.ActionOrgDelete, true},
		{authz.RoleMember, authz.ActionIncidentResolve, true},
		{authz.RoleViewer, authz.ActionMonitorRead, true},
		// …generated from the matrix
	}
	for _, c := range cases {
		ac := authz.AuthContext{Role: c.role, OrgID: uuid.New(), UserID: uuid.New()}
		require.Equal(t, c.want, authz.Can(ac, c.action, nil),
			"%s %s", c.role, c.action)
	}
}

func TestOwnerCannotBeRemovedByAdmin(t *testing.T) {
	admin := authz.AuthContext{Role: authz.RoleAdmin, OrgID: org, UserID: uuid.New()}
	res := &authz.Resource{OrgID: org, TargetRole: authz.RoleOwner}
	require.False(t, authz.Can(admin, authz.ActionMemberRemove, res))
}
```

## 4. Auth & session tests
- Passwords stored only as argon2id hashes; plaintext never appears in the DB.
- Login with a wrong password → `401` with a generic message (no field-level leak).
- Logout revokes the current session; the cookie no longer resolves.
- `logout-all` revokes every session for the user.
- Expired/revoked tokens resolve to no user → `401`.
- Login endpoint is rate-limited → `429` past the threshold.

## 5. Worker tests
- Two worker instances over the same due set never double-process a monitor (`SKIP LOCKED`) — assert each monitor produces exactly one `check_result` per tick.
- `OPEN_THRESHOLD` consecutive failures opens exactly one incident (the partial unique index holds under concurrency).
- `RESOLVE_THRESHOLD` consecutive successes resolves it.
- A timing-out endpoint records a `down` check via the `context.WithTimeout` deadline without stalling the batch.
- The SSRF dialer rejects loopback / private / link-local / `169.254.169.254` targets.

## 6. Threat model (STRIDE-lite) & mitigations

| Threat | Vector | Mitigation |
|--------|--------|-----------|
| **Cross-tenant data access** | Forgotten `WHERE`, IDOR via guessed ids | App scoping **+ RLS**; 404 on foreign ids; isolation suite |
| **Privilege escalation** | Viewer/member performs admin action | `Can()` on every mutation; authz suite; structural guards |
| **Credential theft** | DB leak of passwords/sessions | argon2id hashes; only token *hashes* stored |
| **Session hijack** | XSS reads cookie; CSRF | `HttpOnly`+`Secure` cookie; `SameSite=Lax` + CSRF token/Origin check |
| **Brute force** | Guessing passwords | Rate limiting on auth endpoints → 429 |
| **Existence disclosure** | Probing org/resource ids | 404 (not 403) for non-member access |
| **Public-page overexposure** | Internal fields in public JSON | Strict projection; read-only; no urls/members/audit |
| **SSRF via monitors** | Monitor URL pointed at internal services | `net.Dialer.Control` denies private/loopback/metadata IPs before connecting |
| **Privilege bypass via DB** | App role bypasses RLS | `app_user` is `NOSUPERUSER`, no `BYPASSRLS`; `FORCE ROW LEVEL SECURITY` |

## 7. SSRF note (specific to this product)

Because the worker fetches arbitrary user-supplied URLs, it is an SSRF vector. The Go `http.Client` uses a custom `net.Dialer.Control` hook (see LLD §8.3) that inspects the **resolved IP** and rejects loopback, link-local, RFC-1918 private ranges, and cloud metadata IPs (e.g. `169.254.169.254`) before the connection is made. Checking at dial time (not URL-parse time) defeats DNS-rebinding. Call this out in the write-up — it shows you reasoned about the product's actual attack surface, not a generic checklist.

## 8. Hardening checklist (pre-"done")

- [ ] App connects as `app_user` (`NOSUPERUSER`, no `BYPASSRLS`).
- [ ] `ENABLE` **and** `FORCE ROW LEVEL SECURITY` on every tenant-owned table.
- [ ] Every tenant-owned table has an isolation policy (`USING` + `WITH CHECK`).
- [ ] Passwords: argon2id; tokens: `crypto/rand`, stored hashed.
- [ ] Cookies: `HttpOnly; Secure; SameSite=Lax`; CSRF protection on mutations.
- [ ] Rate limiting on `signup`/`login`.
- [ ] Session revocation (single + all) works and is tested.
- [ ] Audit log written for every gated mutation.
- [ ] SSRF guard on monitor URLs (dial-time IP check).
- [ ] Public status page returns the minimal projection only.
- [ ] Secrets in env/secret store, never in the repo.
- [ ] CI runs the isolation + authz suites against real Postgres with RLS.

## 9. CI gate

The build fails if:
1. Any isolation test returns Org B data.
2. Any authz matrix case diverges from `policy.go`.
3. A `grep`/lint finds a `Role` comparison outside `authz/` (inline role check).
4. Migrations don't apply cleanly 001→007 including RLS + grants.
5. `go vet` / `staticcheck` reports issues, or `sqlc generate` output is stale (drift check).

## 10. The write-up (how to present the proof)

Lead the case study with the deliberate-bug test from §2.3 and the green isolation table-test from §2.2. The narrative arc: *"I assumed I would eventually forget a `WHERE` clause, so I built the database to refuse cross-tenant rows regardless — here's the Go test that strips the filter on purpose and proves the leak still can't happen."* That sentence is what gets a full-stack SWE hired, because it answers the only question that matters: **can I trust this person with production data?**

---

> Note: the security topics above (threat modeling, SSRF, isolation testing) are framed for building and defending your own application — exactly the defensive, production-minded posture reviewers look for.
