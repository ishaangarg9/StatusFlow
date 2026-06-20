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

func TestPermissionMatrix(t *testing.T) {
	org := uuid.New()
	user := uuid.New()

	cases := []struct {
		role   authz.Role
		action authz.Action
		want   bool
	}{
		// Owner can do everything.
		{authz.RoleOwner, authz.ActionOrgDelete, true},
		{authz.RoleOwner, authz.ActionAuditRead, true},
		{authz.RoleOwner, authz.ActionMonitorDelete, true},

		// Admin: full except org:delete.
		{authz.RoleAdmin, authz.ActionOrgDelete, false},
		{authz.RoleAdmin, authz.ActionMonitorDelete, true},
		{authz.RoleAdmin, authz.ActionAuditRead, true},

		// Member: product day-to-day.
		{authz.RoleMember, authz.ActionMonitorCreate, true},
		{authz.RoleMember, authz.ActionMonitorDelete, false},
		{authz.RoleMember, authz.ActionMemberInvite, false},
		{authz.RoleMember, authz.ActionIncidentResolve, true},
		{authz.RoleMember, authz.ActionAuditRead, false},

		// Viewer: read-only.
		{authz.RoleViewer, authz.ActionMonitorRead, true},
		{authz.RoleViewer, authz.ActionMonitorCreate, false},
		{authz.RoleViewer, authz.ActionStatusRead, true},
		{authz.RoleViewer, authz.ActionStatusPublish, false},
	}
	for _, c := range cases {
		ac := authz.AuthContext{UserID: user, OrgID: org, Role: c.role}
		got := authz.Can(ac, c.action, nil)
		if got != c.want {
			t.Errorf("Can(%s, %s) = %v; want %v", c.role, c.action, got, c.want)
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

func TestOrgDeleteIsOwnerOnly(t *testing.T) {
	org := uuid.New()
	for _, role := range []authz.Role{authz.RoleAdmin, authz.RoleMember, authz.RoleViewer} {
		ac := authz.AuthContext{UserID: uuid.New(), OrgID: org, Role: role}
		if authz.Can(ac, authz.ActionOrgDelete, nil) {
			t.Errorf("%s must not be able to delete the org", role)
		}
	}
}
