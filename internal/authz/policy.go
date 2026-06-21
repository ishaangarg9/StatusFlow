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
	ActionOrgRead   Action = "org:read"
	ActionOrgUpdate Action = "org:update"
	ActionOrgDelete Action = "org:delete"

	ActionMemberRead   Action = "member:read"
	ActionMemberInvite Action = "member:invite"
	ActionMemberRemove Action = "member:remove"
	ActionMemberRole   Action = "member:role:update"

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

	ActionAuditRead Action = "audit:read"
)

// AllRoles is the canonical list of roles, for exhaustive iteration (tests,
// admin UIs). Order is highest-to-lowest privilege.
func AllRoles() []Role {
	return []Role{RoleOwner, RoleAdmin, RoleMember, RoleViewer}
}

// ValidRole reports whether r is one of the four known roles. The role set
// lives here (authz owns role identity); callers validate against this rather
// than hand-writing their own role switch.
func ValidRole(r Role) bool {
	switch r {
	case RoleOwner, RoleAdmin, RoleMember, RoleViewer:
		return true
	}
	return false
}

// AssignableViaInvite reports whether r is a role an invitation may grant. owner
// is excluded: ownership is transfer-only (see IsOwnershipTransfer), so an invite
// must never mint a second owner. Keeping this here means role-equality logic
// stays inside authz (CLAUDE.md §2.5).
func AssignableViaInvite(r Role) bool {
	return ValidRole(r) && r != RoleOwner
}

// AllActions is the canonical list of every gated action. Keep it in sync when
// adding an Action constant — the permission-matrix test asserts its expected
// matrix covers exactly this set, so a new action without a matrix entry (and
// thus default-denied silently) fails the build.
func AllActions() []Action {
	return []Action{
		ActionOrgRead, ActionOrgUpdate, ActionOrgDelete,
		ActionMemberRead, ActionMemberInvite, ActionMemberRemove, ActionMemberRole,
		ActionMonitorRead, ActionMonitorCreate, ActionMonitorUpdate, ActionMonitorDelete,
		ActionIncidentRead, ActionIncidentCreate, ActionIncidentUpdate, ActionIncidentResolve,
		ActionStatusRead, ActionStatusPublish,
		ActionAuditRead,
	}
}

func set(actions ...Action) map[Action]bool {
	m := make(map[Action]bool, len(actions))
	for _, a := range actions {
		m[a] = true
	}
	return m
}

// rolePermissions is the single source of truth.
// An action not listed for a role is denied by default.
var rolePermissions = map[Role]map[Action]bool{
	RoleOwner: set(
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
