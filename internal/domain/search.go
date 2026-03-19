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

// LogListOpts encapsulates all query parameters for listing logs
type LogListOpts struct {
	ProjectID uuid.UUID
	Filter    SearchFilter
	Cursor    *time.Time // cursor for pagination (logs before this timestamp)
	Limit     int
}

// LogListResult holds paginated log query results
type LogListResult struct {
	Items      []Log
	NextCursor *time.Time // nil if no more results
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
