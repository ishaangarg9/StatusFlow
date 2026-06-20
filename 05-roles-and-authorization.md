# 05 — Roles & Authorization

Authorization is **role-based (RBAC)** and routed through a single function, `Can(ac, action, resource)`. There are no inline role checks in handlers — a CI rule forbids `Role` comparisons outside the `authz/` package. ABAC is explicitly out of scope at this scale.

## 1. The four roles

| Role | Intent | Cannot |
|------|--------|--------|
| **owner** | Created the org; ultimate authority. Exactly one per org. | n/a (can do everything) |
| **admin** | Runs the org day-to-day: manages monitors, members, incidents, status pages. | Delete the org; remove/demote the owner |
| **member** | Operates the product: creates/edits monitors, opens/resolves incidents. | Manage members; delete monitors; publish status pages; read audit log |
| **viewer** | Read-only internal access (stakeholders). | Any mutation |

Role is stored on the `memberships` row, so the **same user can hold different roles in different orgs**. The active role is whatever their membership in the *active* org says.

## 2. Actions

Actions are namespaced `resource:verb` strings — the vocabulary the whole system shares (HTTP layer, `Can()`, audit log).

```
org:read           org:update        org:delete
member:read        member:invite     member:remove      member:role:update
monitor:read       monitor:create    monitor:update     monitor:delete
incident:read      incident:create   incident:update    incident:resolve
statuspage:read    statuspage:publish
audit:read
```

## 3. The permission matrix

✓ = allowed, ✗ = denied.

| Action | owner | admin | member | viewer |
|--------|:-----:|:-----:|:------:|:------:|
| `org:read` | ✓ | ✓ | ✓ | ✓ |
| `org:update` | ✓ | ✓ | ✗ | ✗ |
| `org:delete` | ✓ | ✗ | ✗ | ✗ |
| `member:read` | ✓ | ✓ | ✓ | ✓ |
| `member:invite` | ✓ | ✓ | ✗ | ✗ |
| `member:remove` | ✓ | ✓ | ✗ | ✗ |
| `member:role:update` | ✓ | ✓ | ✗ | ✗ |
| `monitor:read` | ✓ | ✓ | ✓ | ✓ |
| `monitor:create` | ✓ | ✓ | ✓ | ✗ |
| `monitor:update` | ✓ | ✓ | ✓ | ✗ |
| `monitor:delete` | ✓ | ✓ | ✗ | ✗ |
| `incident:read` | ✓ | ✓ | ✓ | ✓ |
| `incident:create` | ✓ | ✓ | ✓ | ✗ |
| `incident:update` | ✓ | ✓ | ✓ | ✗ |
| `incident:resolve` | ✓ | ✓ | ✓ | ✗ |
| `statuspage:read` | ✓ | ✓ | ✓ | ✓ |
| `statuspage:publish` | ✓ | ✓ | ✗ | ✗ |
| `audit:read` | ✓ | ✓ | ✗ | ✗ |

### 3.1 Structural rules beyond the matrix
These hold regardless of the matrix and are coded as explicit guards in `Can()`:

1. **Owner is irremovable/indemotable by others.** `member:remove` and `member:role:update` targeting the current owner are denied for everyone except the owner performing an explicit *ownership transfer*.
2. **Exactly one owner.** Ownership transfer is atomic: promote target to owner, demote self to admin, in one transaction.
3. **No cross-org resource.** If `resource.OrgID != ac.OrgID`, `Can()` returns `false` no matter the role — defense in depth on top of RLS.
4. **`org:delete` is owner-only**, even if a future matrix edit slips.

## 4. The matrix as data (`authz/policy.go`)

```go
package authz

type Role string
const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
)

type Action string
const (
	ActionOrgRead       Action = "org:read"
	ActionOrgUpdate     Action = "org:update"
	ActionOrgDelete     Action = "org:delete"
	ActionMemberRead    Action = "member:read"
	ActionMemberInvite  Action = "member:invite"
	ActionMemberRemove  Action = "member:remove"
	ActionMemberRole    Action = "member:role:update"
	ActionMonitorRead   Action = "monitor:read"
	ActionMonitorCreate Action = "monitor:create"
	ActionMonitorUpdate Action = "monitor:update"
	ActionMonitorDelete Action = "monitor:delete"
	ActionIncidentRead    Action = "incident:read"
	ActionIncidentCreate  Action = "incident:create"
	ActionIncidentUpdate  Action = "incident:update"
	ActionIncidentResolve Action = "incident:resolve"
	ActionStatusRead    Action = "statuspage:read"
	ActionStatusPublish Action = "statuspage:publish"
	ActionAuditRead     Action = "audit:read"
)

func set(actions ...Action) map[Action]bool {
	m := make(map[Action]bool, len(actions))
	for _, a := range actions {
		m[a] = true
	}
	return m
}

// The single source of truth. Anything not listed is denied by default.
var rolePermissions = map[Role]map[Action]bool{
	RoleOwner: set( // everything
		ActionOrgRead, ActionOrgUpdate, ActionOrgDelete,
		ActionMemberRead, ActionMemberInvite, ActionMemberRemove, ActionMemberRole,
		ActionMonitorRead, ActionMonitorCreate, ActionMonitorUpdate, ActionMonitorDelete,
		ActionIncidentRead, ActionIncidentCreate, ActionIncidentUpdate, ActionIncidentResolve,
		ActionStatusRead, ActionStatusPublish, ActionAuditRead,
	),
	RoleAdmin: set(
		ActionOrgRead, ActionOrgUpdate,
		ActionMemberRead, ActionMemberInvite, ActionMemberRemove, ActionMemberRole,
		ActionMonitorRead, ActionMonitorCreate, ActionMonitorUpdate, ActionMonitorDelete,
		ActionIncidentRead, ActionIncidentCreate, ActionIncidentUpdate, ActionIncidentResolve,
		ActionStatusRead, ActionStatusPublish, ActionAuditRead,
	),
	RoleMember: set(
		ActionOrgRead, ActionMemberRead,
		ActionMonitorRead, ActionMonitorCreate, ActionMonitorUpdate,
		ActionIncidentRead, ActionIncidentCreate, ActionIncidentUpdate, ActionIncidentResolve,
		ActionStatusRead,
	),
	RoleViewer: set(
		ActionOrgRead, ActionMemberRead, ActionMonitorRead, ActionIncidentRead, ActionStatusRead,
	),
}
```

## 5. The `Can()` function (`authz/can.go`)

```go
type AuthContext struct {
	UserID uuid.UUID
	OrgID  uuid.UUID
	Role   Role
}

type Resource struct {
	OrgID      uuid.UUID
	TargetRole Role // set for member-management actions
}

func Can(ac AuthContext, action Action, res *Resource) bool {
	// 1. matrix check (default-deny)
	if !rolePermissions[ac.Role][action] {
		return false
	}
	// 2. cross-org guard — belt to RLS's suspenders
	if res != nil && res.OrgID != uuid.Nil && res.OrgID != ac.OrgID {
		return false
	}
	// 3. structural guards
	switch action {
	case ActionOrgDelete:
		return ac.Role == RoleOwner
	case ActionMemberRemove, ActionMemberRole:
		if res != nil && res.TargetRole == RoleOwner && ac.Role != RoleOwner {
			return false
		}
	}
	return true
}
```

### 5.1 Call sites
Exactly one shape, everywhere:
```go
if !authz.Can(ac, authz.ActionMonitorDelete, &authz.Resource{OrgID: monitor.OrgID}) {
	return Forbidden() // 403
}
```

## 6. Where authorization lives vs. where isolation lives

Two different jobs; never confuse them:

| Concern | Question it answers | Enforced by |
|---------|---------------------|-------------|
| **Isolation (tenancy)** | *Is this row in my org at all?* | `org_id` scoping + Postgres RLS |
| **Authorization (RBAC)** | *Given it's my org, may my role do this?* | `Can()` |

A `viewer` in Org A and an `admin` in Org A are both fully isolated from Org B by RLS; `Can()` then decides what each may do **within** Org A. Isolation failures are breaches; authorization failures are `403`s. The test suite covers both axes independently.

## 7. Auditing role-sensitive actions

Every action that `Can()` gates and that mutates state writes an `audit_logs` row: `(org_id, actor_user_id, action, resource_type, resource_id, metadata)`. This makes "who changed this monitor / removed this member / published this page" answerable, and `audit:read` is itself an owner/admin-gated action.

## 8. Future extension points (kept out of scope deliberately)

- **Custom roles / per-resource grants** → moves toward ABAC; not needed now.
- **Org-level feature flags affecting permissions** → layer on top of `Can()` later without touching call sites, since they all funnel through one function.
- **Service accounts / API tokens** → model as a principal with a role, reusing the same `Can()`.
