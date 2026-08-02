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

// SessionSummary is a session with the counts a list needs beside it. The
// counts come from the logs, not from the session row, so they stay right
// however late a batch arrives.
type SessionSummary struct {
	Session
	Logs    int64
	Errors  int64
	Network int64
}

// Duration is how long the app was up, as far as anything was reported. It is
// last-seen minus started, so a session still running reads as its length so
// far rather than as zero.
func (s SessionSummary) Duration() time.Duration {
	if s.LastSeenAt.Before(s.StartedAt) {
		return 0
	}
	return s.LastSeenAt.Sub(s.StartedAt)
}

// Device is one install, which is one phone: every session sharing an
// installation id, rolled up. The identity is the random id the SDK generates
// once, so this identifies a device without identifying anyone.
type Device struct {
	InstallationID uuid.UUID

	// The most recent session's answer for each. A phone that was renamed, or
	// updated to a new OS or app version, should read as what it is now.
	DeviceModel *string
	OSName      *string
	OSVersion   *string
	AppVersion  *string
	BuildNumber *string
	LastUserID  *string

	Sessions  int64
	Logs      int64
	Errors    int64
	FirstSeen time.Time
	LastSeen  time.Time
}

// DeviceLabel and UserLabel read the same as a session's, by borrowing one.
func (d Device) DeviceLabel() string {
	return Session{DeviceModel: d.DeviceModel, OSName: d.OSName, OSVersion: d.OSVersion}.DeviceLabel()
}

func (d Device) UserLabel() string {
	return Session{UserID: d.LastUserID}.UserLabel()
}

// AppLabel is "3.11.2+418", the way a Flutter version reads. The build number
// is what makes two releases of the same version tellable apart.
func (d Device) AppLabel() string {
	version := derefOr(d.AppVersion, "")
	build := derefOr(d.BuildNumber, "")
	switch {
	case version == "" && build == "":
		return "unknown"
	case build == "":
		return version
	case version == "":
		return "+" + build
	default:
		return version + "+" + build
	}
}

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
