package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Session is one app launch. Until now a session was only a UUID stamped on
// every log row, which left nowhere to record what device it ran on or who was
// using it — so "this phone keeps failing" was an unanswerable question.
//
// Keyed on the client's own session id rather than a server-generated one, so a
// batch that arrives twice updates the same row instead of making a second.
type Session struct {
	ID        uuid.UUID
	ProjectID uuid.UUID

	// InstallationID is stable across launches of the same install and carries
	// no personal data — it is a random value the SDK generates once. It is
	// what makes a device list possible without identifying anyone.
	InstallationID *uuid.UUID

	// UserID is opt-in, set only by setUser(), never inferred. An opaque string
	// Code Scout stores and never parses, so a team can hash it if they like.
	UserID *string

	DeviceModel *string
	OSName      *string
	OSVersion   *string
	AppVersion  *string
	BuildNumber *string

	// Metadata carries traits and whatever the developer attached. Stored per
	// session rather than per user on purpose: a debugging tool needs to know
	// the user was on plan:free *when it broke*, not what they are on now.
	Metadata *json.RawMessage

	StartedAt  time.Time
	LastSeenAt time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// DeviceLabel is how a device reads in a list: "Pixel 7 · Android 14".
func (s Session) DeviceLabel() string {
	model := derefOr(s.DeviceModel, "Unknown device")
	os := derefOr(s.OSName, "")
	version := derefOr(s.OSVersion, "")

	switch {
	case os == "" && version == "":
		return model
	case version == "":
		return model + " · " + os
	default:
		return model + " · " + os + " " + version
	}
}

// UserLabel names the person, or says plainly that nobody was identified.
// "anonymous" rather than blank, because a missing user is a real answer:
// identity is opt-in and never inferred.
func (s Session) UserLabel() string {
	if s.UserID == nil || *s.UserID == "" {
		return "anonymous"
	}
	return *s.UserID
}

// IsIdentified reports whether setUser() was ever called on this session.
func (s Session) IsIdentified() bool { return s.UserID != nil && *s.UserID != "" }

// IncomingSession is the session record the SDK re-sends with every sync batch.
// Tiny, idempotent, and it removes a whole class of ordering bugs: no batch has
// to be the first one, and a failed first upload does not leave later logs
// belonging to a session that was never announced.
type IncomingSession struct {
	ID             uuid.UUID        `json:"id"`
	InstallationID *uuid.UUID       `json:"installation_id"`
	UserID         *string          `json:"user_id"`
	DeviceModel    *string          `json:"device_model"`
	OSName         *string          `json:"os_name"`
	OSVersion      *string          `json:"os_version"`
	AppVersion     *string          `json:"app_version"`
	BuildNumber    *string          `json:"build_number"`
	Metadata       *json.RawMessage `json:"metadata"`
	StartedAt      *time.Time       `json:"started_at"`
	LastSeenAt     *time.Time       `json:"last_seen_at"`
}

// ToSession fills in the times the SDK left out. A session with no start is
// treated as starting now rather than at the zero time, which would sort it
// before everything ever recorded.
func (in IncomingSession) ToSession(projectID uuid.UUID, now time.Time) Session {
	started := now
	if in.StartedAt != nil && !in.StartedAt.IsZero() {
		started = *in.StartedAt
	}
	lastSeen := now
	if in.LastSeenAt != nil && !in.LastSeenAt.IsZero() {
		lastSeen = *in.LastSeenAt
	}

	return Session{
		ID:             in.ID,
		ProjectID:      projectID,
		InstallationID: in.InstallationID,
		UserID:         in.UserID,
		DeviceModel:    in.DeviceModel,
		OSName:         in.OSName,
		OSVersion:      in.OSVersion,
		AppVersion:     in.AppVersion,
		BuildNumber:    in.BuildNumber,
		Metadata:       in.Metadata,
		StartedAt:      started,
		LastSeenAt:     lastSeen,
	}
}

func derefOr(s *string, fallback string) string {
	if s == nil || *s == "" {
		return fallback
	}
	return *s
}
