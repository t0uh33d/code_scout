package domain

import (
	"time"

	"github.com/google/uuid"
)

type Project struct {
	ID          uuid.UUID
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

type ProjectSecret struct {
	ID        uuid.UUID
	ProjectID string
	SecretKey string
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

type CreateProjectOpts struct {
	Name        string `json:"name"`
	Description string `json:"description"`

	// CreatedBy becomes the project's first maintainer. Without it an admin
	// would create a project and immediately lose sight of it, since visibility
	// comes from membership. Never taken from the request body.
	CreatedBy uuid.UUID `json:"-"`
	// Members are added alongside the creator, from the wizard's Access step.
	Members []ProjectMemberInput `json:"-"`
}

// ProjectMemberInput is one row of the Access step.
type ProjectMemberInput struct {
	UserID uuid.UUID
	Level  ProjectLevel
}

type ProjectDetails struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`

	// Only populated when the project is created — this is the one moment the
	// plaintext secret is available to return. Retrieving it later needs the
	// project settings screen.
	SecretKey string `json:"secret_key,omitempty"`
}

// UpdateProjectOpts are the editable fields on the settings screen.
type UpdateProjectOpts struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type ProjectListItem struct {
	ID          uuid.UUID
	Name        string
	Description string
	CreatedAt   time.Time
	IsFavorite  bool
}

type ProjectListOpts struct {
	Search   string
	Page     int
	PageSize int

	// UserID scopes the IsFavorite flag, and FavoritesOnly narrows the list to
	// that user's favourites. Favourites are per user, so both are needed.
	UserID        uuid.UUID
	FavoritesOnly bool

	// RestrictToMemberships limits the list to projects the user belongs to.
	// True for everyone except a super admin, who sees the instance whole.
	// Deliberately a positive flag rather than "IsSuperAdmin": a caller that
	// forgets to set it gets a full list, which is the wrong default, so every
	// call site is written to set it explicitly.
	RestrictToMemberships bool
}

// ScopeToUser fills in the visibility fields from the signed-in account. Call
// sites use this rather than setting UserID and RestrictToMemberships by hand,
// so no handler can forget one and quietly widen the list.
//
// A nil user restricts with no id, which resolves to an empty list. Failing
// closed is the point: the alternative leaks every project.
func (o *ProjectListOpts) ScopeToUser(u *User) {
	if u == nil {
		o.RestrictToMemberships = true
		return
	}
	o.UserID = u.ID
	o.RestrictToMemberships = u.Role != RoleSuperAdmin
}

type ProjectListResult struct {
	Items      []ProjectListItem
	TotalCount int64
	Page       int
	PageSize   int
	TotalPages int
}
