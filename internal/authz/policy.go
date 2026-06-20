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
