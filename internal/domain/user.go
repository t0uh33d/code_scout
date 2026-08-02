package domain

import (
	"time"

	"github.com/google/uuid"
)

// Role is what an account may do instance-wide. It is about capabilities, not
// seniority: Admin means "may create projects and accounts", and notably does
// NOT mean "sees every project". Visibility comes from project membership for
// everyone except a super admin.
type Role string

const (
	// RoleSuperAdmin sees every project and changes instance settings. Several
	// may exist; the last one cannot be removed or demoted.
	RoleSuperAdmin Role = "super_admin"
	// RoleAdmin may create projects and accounts. Creating a project makes them
	// its maintainer. Sees only projects they belong to.
	RoleAdmin Role = "admin"
	// RoleMember may not create projects. Sees only projects they belong to.
	RoleMember Role = "member"
)

func (r Role) Valid() bool {
	return r == RoleSuperAdmin || r == RoleAdmin || r == RoleMember
}

// Outranks reports whether r may reset or remove another account holding other.
// You may act on roles strictly below your own.
func (r Role) Outranks(other Role) bool {
	rank := map[Role]int{RoleMember: 1, RoleAdmin: 2, RoleSuperAdmin: 3}
	return rank[r] > rank[other]
}

// Label is the human name for the role, used in the UI and in errors.
func (r Role) Label() string {
	switch r {
	case RoleSuperAdmin:
		return "Super admin"
	case RoleAdmin:
		return "Admin"
	default:
		return "Member"
	}
}

// ProjectLevel is what someone may do inside one project they belong to.
type ProjectLevel string

const (
	// LevelViewer reads logs, sessions, errors and network calls, exports, and
	// watches live sessions.
	LevelViewer ProjectLevel = "viewer"
	// LevelMaintainer adds project settings, revealing and rotating that
	// project's secret, and managing who else is on it. Deleting the project
	// additionally requires the Admin role.
	LevelMaintainer ProjectLevel = "maintainer"
)

func (l ProjectLevel) Valid() bool {
	return l == LevelViewer || l == LevelMaintainer
}

func (l ProjectLevel) Label() string {
	if l == LevelMaintainer {
		return "Maintainer"
	}
	return "Viewer"
}

// User represents a dashboard user account.
//
// Email is the login identifier. No mail is ever sent, so it is only an
// identifier and a way for an admin to know who a row belongs to.
type User struct {
	ID           uuid.UUID
	Name         string
	Email        string
	Role         Role
	PasswordHash string
	// MustChangePassword is set when an account is created with a generated
	// temporary password, or after someone resets it.
	MustChangePassword bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
	DeletedAt          *time.Time
}

// IsSuperAdmin is a readability helper; super admin is checked constantly.
func (u *User) IsSuperAdmin() bool { return u != nil && u.Role == RoleSuperAdmin }

// CanCreateProjects reports whether this account may create projects.
func (u *User) CanCreateProjects() bool {
	return u != nil && (u.Role == RoleSuperAdmin || u.Role == RoleAdmin)
}

// ProjectMember is one account's access to one project.
type ProjectMember struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	ProjectID uuid.UUID
	Level     ProjectLevel
	CreatedAt time.Time

	// Populated when listing a project's members for display.
	Name  string
	Email string
	Role  Role
}

// ProjectAccess is the resolved answer to "what may this user do in this
// project", combining their instance role with any membership row.
type ProjectAccess struct {
	CanRead bool
	// CanManage covers project settings, the secret, and membership.
	CanManage bool
	CanDelete bool
	// Level is the membership level, empty when access comes from being a
	// super admin rather than from a membership row.
	Level ProjectLevel
}

// UserSession represents an active web session for a user.
type UserSession struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Token     string // UUID used as cookie value
	ExpiresAt time.Time
	CreatedAt time.Time
}

// CreateMemberOpts is the add-member form.
type CreateMemberOpts struct {
	Name     string
	Email    string
	Role     Role
	Projects []ProjectAssignment
}

// ProjectAssignment grants one project at one level.
type ProjectAssignment struct {
	UserID    uuid.UUID
	ProjectID uuid.UUID
	Level     ProjectLevel
}

// AuthOpts are the form fields submitted on the auth page.
// Name is only present on first run, where the account is being created.
type AuthOpts struct {
	Name            string `json:"name"`
	Email           string `json:"email"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
}
