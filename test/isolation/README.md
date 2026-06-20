# Cross-tenant isolation suite — the proof

This is the centerpiece of the project. Build it out per doc 07 §2.

## Required tests

1. **`TestCrossTenantAccessIsImpossible`** (doc 07 §2.2)
   Table-driven over every org-scoped endpoint; signed in as Org A, every read or
   mutate of Org B's data returns 403/404/empty — never Org B's data.

2. **`TestRLSBlocksLeakEvenWithoutAppFilter`** (doc 07 §2.3)
   Strip the `WHERE org_id` from a query inside `WithOrgTx(OrgA)` and assert the
   database STILL refuses Org B's rows. Lead the case study with this one.

3. **`TestForeignResourceIdIsNotFound`** (doc 07 §2.4)
   Path `/api/orgs/{A}/monitors/{B's monitor id}` → 404, never 200 with B's data.

## Fixture

Per doc 07 §2.1:

```
Org A: owner=alice, member=bob
Org B: owner=carol, member=dave
Each org seeded with monitors, incidents, status pages, audit entries.
```

Use `testcontainers-go` or a docker-compose test database so the DB is real and
RLS is on. Connect as `app_user`. Mocks would skip the exact layer being proven.

## CI gate

CI fails if any case returns Org B data, or if a matrix case diverges from
`internal/authz/policy.go` (see `test/authz/matrix_test.go`).
