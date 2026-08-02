package domain

import (
	"time"

	"github.com/google/uuid"
)

// SearchFilter represents a parsed search query for logs
type SearchFilter struct {
	Level     *string    // exact match on level
	Tags      []string   // logs must contain all of these tags
	TextQuery string     // LIKE match on message field
	IsNetwork *bool      // filter network calls only
	SessionID *uuid.UUID
	RequestID *uuid.UUID
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
