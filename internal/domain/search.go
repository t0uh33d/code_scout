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
	Hour       time.Time
	TotalCount int64
	ErrorCount int64
}

// LogStatsResult for a project's health sparkline
type LogStatsResult struct {
	Buckets     []LogStatsBucket
	TotalLogs   int64
	TotalErrors int64
}
