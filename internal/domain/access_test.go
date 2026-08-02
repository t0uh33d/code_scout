package domain

import (
	"testing"

	"github.com/google/uuid"
)

func user(role Role) *User { return &User{ID: uuid.New(), Role: role} }

func member(level ProjectLevel) *ProjectMember { return &ProjectMember{Level: level} }

func TestResolveProjectAccess(t *testing.T) {
	cases := []struct {
		name       string
		user       *User
		membership *ProjectMember
		read       bool
		manage     bool
		del        bool
	}{
		// The only role that bypasses membership.
		{"super admin without membership", user(RoleSuperAdmin), nil, true, true, true},
		{"super admin with membership", user(RoleSuperAdmin), member(LevelViewer), true, true, true},

		// The decision that surprises people: an admin does not see a project
		// they were never added to, and does not learn it exists.
		{"admin without membership", user(RoleAdmin), nil, false, false, false},
		{"admin as viewer", user(RoleAdmin), member(LevelViewer), true, false, false},
		{"admin as maintainer", user(RoleAdmin), member(LevelMaintainer), true, true, true},

		{"member without membership", user(RoleMember), nil, false, false, false},
		{"member as viewer", user(RoleMember), member(LevelViewer), true, false, false},
		// Can rotate the secret, cannot destroy the project.
		{"member as maintainer", user(RoleMember), member(LevelMaintainer), true, true, false},

		{"nil user", nil, member(LevelMaintainer), false, false, false},
	}

	for _, c := range cases {
		got := ResolveProjectAccess(c.user, c.membership)
		if got.CanRead != c.read || got.CanManage != c.manage || got.CanDelete != c.del {
			t.Errorf("%s: read=%v manage=%v delete=%v, want %v/%v/%v",
				c.name, got.CanRead, got.CanManage, got.CanDelete, c.read, c.manage, c.del)
		}
	}
}

func TestRoleOutranks(t *testing.T) {
	cases := []struct {
		actor, target Role
		want          bool
	}{
		{RoleSuperAdmin, RoleAdmin, true},
		{RoleSuperAdmin, RoleMember, true},
		{RoleAdmin, RoleMember, true},
		// Nobody outranks their own role, so an admin cannot reset another
		// admin and a super admin cannot reset another super admin.
		{RoleSuperAdmin, RoleSuperAdmin, false},
		{RoleAdmin, RoleAdmin, false},
		{RoleAdmin, RoleSuperAdmin, false},
		{RoleMember, RoleMember, false},
		{RoleMember, RoleAdmin, false},
	}
	for _, c := range cases {
		if got := c.actor.Outranks(c.target); got != c.want {
			t.Errorf("%s outranks %s = %v, want %v", c.actor, c.target, got, c.want)
		}
	}
}

func TestCanManageAccount(t *testing.T) {
	admin := user(RoleAdmin)
	if CanManageAccount(admin, admin) {
		t.Error("nobody may reset or remove themselves through the account controls")
	}
	if !CanManageAccount(admin, user(RoleMember)) {
		t.Error("an admin should be able to manage a member")
	}
	if CanManageAccount(admin, user(RoleAdmin)) {
		t.Error("an admin must not be able to manage another admin")
	}
	if CanManageAccount(user(RoleMember), user(RoleMember)) {
		t.Error("a member may not manage anyone")
	}
	if CanManageAccount(nil, user(RoleMember)) || CanManageAccount(admin, nil) {
		t.Error("nil operands must be refused")
	}
}

func TestRoleAndLevelValidation(t *testing.T) {
	for _, r := range []Role{RoleSuperAdmin, RoleAdmin, RoleMember} {
		if !r.Valid() {
			t.Errorf("%s should be valid", r)
		}
	}
	if Role("owner").Valid() {
		t.Error("an unknown role must be refused, or a bad form value becomes a privilege")
	}
	if !LevelViewer.Valid() || !LevelMaintainer.Valid() {
		t.Error("both levels should be valid")
	}
	if ProjectLevel("admin").Valid() {
		t.Error("an unknown level must be refused")
	}
}
