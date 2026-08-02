package domain

// ResolveProjectAccess is the one place that answers "what may this user do in
// this project". Every route that touches a project goes through it, so the
// rules live here rather than being re-derived per handler.
//
// membership is nil when the user has no row for the project.
func ResolveProjectAccess(user *User, membership *ProjectMember) ProjectAccess {
	if user == nil {
		return ProjectAccess{}
	}

	// A super admin sees everything without a membership row. This is the only
	// role that bypasses membership, and it is why the check is first.
	if user.Role == RoleSuperAdmin {
		return ProjectAccess{CanRead: true, CanManage: true, CanDelete: true}
	}

	if membership == nil {
		// No row means no access, whatever the role. An admin who did not
		// create this project and was not added to it cannot see it, and does
		// not learn that it exists.
		return ProjectAccess{}
	}

	maintainer := membership.Level == LevelMaintainer
	return ProjectAccess{
		CanRead:   true,
		CanManage: maintainer,
		// Deleting is irreversible and its consequences outlive the project, so
		// it needs the instance-level Admin role on top of maintaining this
		// project. A member who maintains a project can rotate its secret but
		// cannot destroy it.
		CanDelete: maintainer && user.Role == RoleAdmin,
		Level:     membership.Level,
	}
}

// CanManageAccount reports whether actor may reset or remove target. Account
// operations are never delegated by project: maintaining a project does not let
// you touch the passwords of people on it.
func CanManageAccount(actor *User, target *User) bool {
	if actor == nil || target == nil {
		return false
	}
	if actor.ID == target.ID {
		return false
	}
	return actor.Role.Outranks(target.Role)
}
