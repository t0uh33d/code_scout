package domain

import (
	"time"

	"github.com/google/uuid"
)

type Project struct {
	ID          uuid.UUID
	Name        string
	Description string

	// SessionSampleRate is the percentage of launches that record anything at
	// all, 0 to 100. Whole sessions rather than individual logs, so a session
	// that is kept keeps its whole timeline and nothing reads as a gap.
	SessionSampleRate int

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

// FullSampleRate is every session, which is what a new project gets. Sampling
// is something you reach for when volume demands it, not a tax on a project
// that has not hit that yet.
const FullSampleRate = 100

// SampleFraction is the rate as the SDK wants it: 0.0 to 1.0.
func (p *Project) SampleFraction() float64 {
	return float64(p.SessionSampleRate) / 100
}

// ValidSampleRate rejects a rate outside the range rather than clamping it.
// Clamping a typo to 100 would silently keep the firehose on.
func ValidSampleRate(n int) bool { return n >= 0 && n <= 100 }

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

	// A pointer because 0 is a real rate meaning "record nothing". As a plain
	// int, any caller that did not set the field would silently switch the
	// project off; nil says "leave it alone" and cannot be arrived at by
	// forgetting something.
	SessionSampleRate *int `json:"session_sample_rate"`
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
