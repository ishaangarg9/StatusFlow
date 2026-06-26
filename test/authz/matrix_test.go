// Package authz_test drives the entire permission matrix from doc 05 as a
// table test. Adding an action without a policy.go entry must fail loudly
// (default-deny), and the structural guards (owner-only delete, owner
// protection) must hold regardless of the matrix.
package authz_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/ishaangarg9/statusflow/internal/authz"
)

// wantMatrix is the EXHAUSTIVE permission matrix from doc 05 §3, transcribed by
// hand as the source of truth. The test below asserts Can() reproduces every
// cell, so any drift in internal/authz/policy.go fails the build.
//
// The roles slice per action lists the roles allowed; all others are denied.
var wantMatrix = map[authz.Action][]authz.Role{
	authz.ActionOrgRead:         {authz.RoleOwner, authz.RoleAdmin, authz.RoleMember, authz.RoleViewer},
	authz.ActionOrgUpdate:       {authz.RoleOwner, authz.RoleAdmin},
	authz.ActionOrgDelete:       {authz.RoleOwner},
	authz.ActionMemberRead:      {authz.RoleOwner, authz.RoleAdmin, authz.RoleMember, authz.RoleViewer},
	authz.ActionMemberInvite:    {authz.RoleOwner, authz.RoleAdmin},
	authz.ActionMemberRemove:    {authz.RoleOwner, authz.RoleAdmin},
	authz.ActionMemberRole:      {authz.RoleOwner, authz.RoleAdmin},
	authz.ActionMonitorRead:     {authz.RoleOwner, authz.RoleAdmin, authz.RoleMember, authz.RoleViewer},
	authz.ActionMonitorCreate:   {authz.RoleOwner, authz.RoleAdmin, authz.RoleMember},
	authz.ActionMonitorUpdate:   {authz.RoleOwner, authz.RoleAdmin, authz.RoleMember},
	authz.ActionMonitorDelete:   {authz.RoleOwner, authz.RoleAdmin},
	authz.ActionIncidentRead:    {authz.RoleOwner, authz.RoleAdmin, authz.RoleMember, authz.RoleViewer},
	authz.ActionIncidentCreate:  {authz.RoleOwner, authz.RoleAdmin, authz.RoleMember},
	authz.ActionIncidentUpdate:  {authz.RoleOwner, authz.RoleAdmin, authz.RoleMember},
	authz.ActionIncidentResolve: {authz.RoleOwner, authz.RoleAdmin, authz.RoleMember},
	authz.ActionStatusRead:      {authz.RoleOwner, authz.RoleAdmin, authz.RoleMember, authz.RoleViewer},
	authz.ActionStatusPublish:   {authz.RoleOwner, authz.RoleAdmin},
	authz.ActionAuditRead:       {authz.RoleOwner, authz.RoleAdmin},
	authz.ActionBillingRead:     {authz.RoleOwner, authz.RoleAdmin},
	authz.ActionBillingManage:   {authz.RoleOwner},
}

func TestPermissionMatrix(t *testing.T) {
	org := uuid.New()
	user := uuid.New()

	// Guard: the expected matrix must cover exactly the actions authz exposes,
	// so adding an Action (to AllActions) without a matrix row fails here rather
	// than being silently default-denied.
	if len(wantMatrix) != len(authz.AllActions()) {
		t.Fatalf("wantMatrix has %d actions but authz.AllActions() has %d — add the new action to the test matrix",
			len(wantMatrix), len(authz.AllActions()))
	}

	for _, action := range authz.AllActions() {
		allowed, ok := wantMatrix[action]
		if !ok {
			t.Fatalf("action %q missing from wantMatrix", action)
		}
		allow := make(map[authz.Role]bool, len(allowed))
		for _, r := range allowed {
			allow[r] = true
		}
		for _, role := range authz.AllRoles() {
			ac := authz.AuthContext{UserID: user, OrgID: org, Role: role}
			got := authz.Can(ac, action, nil)
			want := allow[role]
			if got != want {
				t.Errorf("Can(%s, %s) = %v; want %v", role, action, got, want)
			}
		}
	}
}

func TestCrossOrgGuardDeniesEvenForOwner(t *testing.T) {
	ac := authz.AuthContext{UserID: uuid.New(), OrgID: uuid.New(), Role: authz.RoleOwner}
	otherOrg := uuid.New()
	if authz.Can(ac, authz.ActionMonitorRead, &authz.Resource{OrgID: otherOrg}) {
		t.Fatal("owner of A must not access org B even with read action")
	}
}

func TestOwnerCannotBeRemovedByAdmin(t *testing.T) {
	org := uuid.New()
	admin := authz.AuthContext{UserID: uuid.New(), OrgID: org, Role: authz.RoleAdmin}
	res := &authz.Resource{OrgID: org, TargetRole: authz.RoleOwner}
	if authz.Can(admin, authz.ActionMemberRemove, res) {
		t.Fatal("admin must not remove the owner")
	}
	if authz.Can(admin, authz.ActionMemberRole, res) {
		t.Fatal("admin must not change the owner's role")
	}
}

// Regression for review finding #1: a non-owner (admin) must NOT be able to
// grant the owner role — doing so previously minted a second owner and let an
// admin escalate an arbitrary member to the top role.
func TestOnlyOwnerCanGrantOwnership(t *testing.T) {
	org := uuid.New()
	target := &authz.Resource{OrgID: org, TargetRole: authz.RoleMember, NewRole: authz.RoleOwner}

	for _, role := range []authz.Role{authz.RoleAdmin, authz.RoleMember, authz.RoleViewer} {
		ac := authz.AuthContext{UserID: uuid.New(), OrgID: org, Role: role}
		if authz.Can(ac, authz.ActionMemberRole, target) {
			t.Errorf("%s must not be able to promote a member to owner", role)
		}
	}
	owner := authz.AuthContext{UserID: uuid.New(), OrgID: org, Role: authz.RoleOwner}
	if !authz.Can(owner, authz.ActionMemberRole, target) {
		t.Fatal("owner must be able to transfer ownership (promote a member to owner)")
	}
}

// Regression for review finding #2: an owner must NOT be able to demote an
// owner (including themselves) directly — that would leave the org with zero
// owners. The only way to stop being owner is to transfer ownership.
func TestOwnerCannotBeDirectlyDemoted(t *testing.T) {
	org := uuid.New()
	owner := authz.AuthContext{UserID: uuid.New(), OrgID: org, Role: authz.RoleOwner}
	for _, to := range []authz.Role{authz.RoleAdmin, authz.RoleMember, authz.RoleViewer} {
		res := &authz.Resource{OrgID: org, TargetRole: authz.RoleOwner, NewRole: to}
		if authz.Can(owner, authz.ActionMemberRole, res) {
			t.Errorf("owner must not be able to directly demote the owner to %s", to)
		}
	}
}

// An owner is never removable directly (transfer first), regardless of who asks.
func TestOwnerCannotBeRemovedByAnyone(t *testing.T) {
	org := uuid.New()
	res := &authz.Resource{OrgID: org, TargetRole: authz.RoleOwner}
	for _, role := range []authz.Role{authz.RoleOwner, authz.RoleAdmin, authz.RoleMember, authz.RoleViewer} {
		ac := authz.AuthContext{UserID: uuid.New(), OrgID: org, Role: role}
		if authz.Can(ac, authz.ActionMemberRemove, res) {
			t.Errorf("%s must not be able to remove an owner", role)
		}
	}
}

// Sanity: ordinary role changes by admin/owner remain allowed.
func TestOrdinaryRoleChangesAllowed(t *testing.T) {
	org := uuid.New()
	for _, role := range []authz.Role{authz.RoleOwner, authz.RoleAdmin} {
		ac := authz.AuthContext{UserID: uuid.New(), OrgID: org, Role: role}
		res := &authz.Resource{OrgID: org, TargetRole: authz.RoleMember, NewRole: authz.RoleAdmin}
		if !authz.Can(ac, authz.ActionMemberRole, res) {
			t.Errorf("%s should be able to change a member to admin", role)
		}
	}
}

func TestOrgDeleteIsOwnerOnly(t *testing.T) {
	org := uuid.New()
	for _, role := range []authz.Role{authz.RoleAdmin, authz.RoleMember, authz.RoleViewer} {
		ac := authz.AuthContext{UserID: uuid.New(), OrgID: org, Role: role}
		if authz.Can(ac, authz.ActionOrgDelete, nil) {
			t.Errorf("%s must not be able to delete the org", role)
		}
	}
}
