package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// SearchFilter represents a parsed search query for logs.
//
// Every control in the log viewer's toolbar edits the query string rather than
// carrying its own state, so this struct is the only description of what is
// being shown — and a pasted URL reproduces the view exactly.
type SearchFilter struct {
	// Levels is an OR: any of these levels matches. Empty means every level,
	// which is what an unfiltered view shows.
	Levels []string
	// Tags is an AND: a log must carry all of them.
	Tags []string
	// ExcludeTags is a NOT: a log carrying any of them is hidden. Applied after
	// Tags, so `tag:checkout -tag:heartbeat` is a sensible pair rather than a
	// contradiction.
	ExcludeTags []string
	TextQuery   string // LIKE match on message field
	IsNetwork   *bool  // filter network calls only
	SessionID   *uuid.UUID
	RequestID   *uuid.UUID
	// Fingerprint pins the view to one error group, which is what the Errors
	// screen's "View in logs" links to. Machine-generated rather than typed,
	// but it lives in the query string like everything else so the filter bar
	// keeps working on top of it and the URL stays shareable.
	Fingerprint *string
	// Since bounds the window. Nil means everything ever recorded.
	Since *time.Time
	// SinceLabel is the preset that produced Since ("24h", "7d"), kept so the
	// toolbar can show which one is active without re-deriving it from a
	// timestamp that has already drifted.
	SinceLabel string

	// Session narrows by facts about the launch rather than about the log.
	Session SessionScope
}

// SessionScope filters logs by what was true of the launch that produced them:
// who was using the app, on what, running which build.
//
// These are the only filters that are not columns on `logs`. They live on
// `sessions`, and the repository turns them into one subquery rather than
// denormalising them onto every log row — which would be faster to read and
// wrong the moment `setUser()` is called, since that attributes a whole session
// retroactively including the logs written before it.
type SessionScope struct {
	// User matches exactly. It is an opaque id the app chose, and a partial
	// match on identifiers finds the wrong person.
	User string
	// Installation matches exactly, for the same reason.
	Installation string
	// AppVersion matches exactly. Prefix matching would be convenient and
	// treacherous: `app_version:3.1` silently including 3.11.2 is the kind of
	// wrong answer nobody checks.
	AppVersion string
	// SDKVersion matches exactly, for the same reason as AppVersion. It is the
	// version of code_scout the app was running, which is how you find every
	// launch still on a build that predates a fix.
	SDKVersion string

	// Device and OS match on a contained substring, case-insensitively. Nobody
	// types "Pixel 7" when they mean every Pixel, and nobody remembers whether
	// the SDK recorded "Android" or "android".
	Device string
	OS     string
}

// Any reports whether anything here is set, which is what decides whether the
// query needs to reach into `sessions` at all.
func (s SessionScope) Any() bool {
	return s.User != "" || s.Installation != "" || s.AppVersion != "" ||
		s.SDKVersion != "" || s.Device != "" || s.OS != ""
}

// sessionField pairs a search keyword with the value currently held for it.
type sessionField struct {
	name  string
	value string
}

// fields lists the scope in a fixed order, so serialising a filter twice gives
// the same string and a shared URL does not churn.
func (s SessionScope) fields() []sessionField {
	return []sessionField{
		{"user", s.User},
		{"device", s.Device},
		{"os", s.OS},
		{"app_version", s.AppVersion},
		{"sdk_version", s.SDKVersion},
		{"installation", s.Installation},
	}
}

// SessionChips is what the filter bar renders: the session filters currently
// applied, each with the query that removes it.
func (f SearchFilter) SessionChips() []SessionChip {
	var out []SessionChip
	for _, sf := range f.Session.fields() {
		if sf.value == "" {
			continue
		}
		out = append(out, SessionChip{
			Field:   sf.name,
			Value:   sf.value,
			Without: f.WithoutSessionField(sf.name).Query(),
		})
	}
	return out
}

// SessionChip is one applied session filter and the way out of it.
type SessionChip struct {
	Field   string
	Value   string
	Without string
}

// WithoutSessionField clears one session filter, which is what the × on its
// chip links to.
func (f SearchFilter) WithoutSessionField(name string) SearchFilter {
	out := f.clone()
	switch name {
	case "user":
		out.Session.User = ""
	case "device":
		out.Session.Device = ""
	case "os":
		out.Session.OS = ""
	case "app_version":
		out.Session.AppVersion = ""
	case "sdk_version":
		out.Session.SDKVersion = ""
	case "installation":
		out.Session.Installation = ""
	}
	return out
}

// HasLevel reports whether a level is currently shown, which is what the level
// toggles render from. No levels set means everything is shown.
func (f SearchFilter) HasLevel(level string) bool {
	if len(f.Levels) == 0 {
		return true
	}
	for _, l := range f.Levels {
		if l == level {
			return true
		}
	}
	return false
}

// TagState is how a tag chip is currently applied.
type TagState int

const (
	TagNeutral TagState = iota
	TagIncluded
	TagExcluded
)

// StateForTag drives the tri-state chips: click once to include, twice to
// exclude, a third time back to neutral.
func (f SearchFilter) StateForTag(tag string) TagState {
	for _, t := range f.Tags {
		if t == tag {
			return TagIncluded
		}
	}
	for _, t := range f.ExcludeTags {
		if t == tag {
			return TagExcluded
		}
	}
	return TagNeutral
}

// LogLevels is every level the SDK can emit, in severity order. The toolbar
// renders toggles from this, so adding a level here adds its toggle.
//
// "all" and "off" are deliberately absent: they are SDK threshold settings, not
// levels a log is ever stored with.
var LogLevels = []string{"fatal", "error", "warning", "info", "debug", "verbose", "system"}

// TagCount is one entry in the tag picker: the tag and how many logs carry it
// inside the current window.
type TagCount struct {
	Tag   string
	Count int64
}

// LogCursor is a keyset pagination cursor. Timestamp alone is not unique
// (a sync batch lands many logs in the same second), so ID — a time-ordered
// UUIDv7 — breaks ties.
type LogCursor struct {
	Time time.Time
	ID   uuid.UUID
}

// LogListOpts encapsulates all query parameters for listing logs
type LogListOpts struct {
	ProjectID uuid.UUID
	Filter    SearchFilter
	Cursor    *LogCursor // cursor for pagination (logs before this position)
	Limit     int
}

// LogListResult holds paginated log query results
type LogListResult struct {
	Items      []Log
	NextCursor *LogCursor // nil if no more results
	HasMore    bool
}

// LogStatsOpts for sparkline data
type LogStatsOpts struct {
	ProjectID uuid.UUID
	Hours     int // how many hours back (default 24)
}

// LogStatsBucket represents one time bucket for sparkline
type LogStatsBucket struct {
	Hour         time.Time
	TotalCount   int64
	ErrorCount   int64
	NetworkCount int64
	FailedCount  int64
}

// LogStatsResult for a project's health sparkline
type LogStatsResult struct {
	Buckets     []LogStatsBucket
	TotalLogs   int64
	TotalErrors int64
}

// ProjectOverview is everything the overview screen counts. It is one query
// over 48 hours: the recent half fills the tiles and the chart, the older half
// only exists so each tile can say how it compares with yesterday.
type ProjectOverview struct {
	Buckets []LogStatsBucket

	Logs     int64
	Errors   int64
	Network  int64
	Failed   int64
	Sessions int64

	// Previous holds the same day before, for the deltas.
	PrevLogs   int64
	PrevErrors int64

	// PeakErrorHour is the busiest error bucket, which the chart calls out.
	// Zero when nothing errored.
	PeakErrorHour   time.Time
	PeakErrorCount  int64
	WindowStartHour time.Time
}

// ErrorDelta is the change in errors against the day before. Positive is worse.
func (o ProjectOverview) ErrorDelta() int64 { return o.Errors - o.PrevErrors }

// LogDelta is the change in log volume against the day before.
func (o ProjectOverview) LogDelta() int64 { return o.Logs - o.PrevLogs }

// ErrorGroup is one distinct problem on the Errors screen: what it is, how
// often it happened, and how many sessions it touched.
//
// "47 times across 12 sessions" is the number that matters. 47 occurrences in
// one session is one user having a bad afternoon; 47 across 40 sessions is an
// outage.
type ErrorGroup struct {
	Fingerprint string
	// SampleMessage is the most recent raw message in the group, so the row
	// shows real text rather than the normalised key.
	SampleMessage string
	Level         string
	Count         int64
	Sessions      int64
	FirstSeen     time.Time
	LastSeen      time.Time
	// LatestLogID links straight to the occurrence you would want to open.
	LatestLogID uuid.UUID
	// The rest describe that same latest occurrence: the session it happened
	// in, the trace it carried, and the tags it was filed under. A group has no
	// stack trace of its own — the most recent one is the one worth reading.
	LatestSessionID uuid.UUID
	StackTrace      *json.RawMessage
	Tags            *json.RawMessage
}
