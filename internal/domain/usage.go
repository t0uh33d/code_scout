package domain

import (
	"time"

	"github.com/google/uuid"
)

// DailyUsage is how much a project has stored on one day.
//
// A counter rather than COUNT(*) over the logs, for two reasons. Cost: the
// count would be an index range scan growing with the day's volume, paid on
// every upload. Correctness: retention soft-deletes rows and they stop being
// counted, so as the day aged the cap would silently reopen and a project
// could store far more than its limit.
type DailyUsage struct {
	ProjectID uuid.UUID
	Day       time.Time
	LogCount  int64
}

// UsageDay is the bucket a moment belongs to.
//
// UTC, always — not the instance timezone. That setting is editable, and a
// movable boundary would retroactively change which day existing rows were
// counted against. The same reasoning the overview's hourly buckets use.
func UsageDay(at time.Time) time.Time {
	utc := at.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

// SecondsUntilNextDay is how long a capped project must wait, in the delta
// seconds a Retry-After header carries.
//
// Delta seconds rather than an HTTP date: a phone's clock is routinely wrong,
// and the date form would make the pause depend on the client's skew. Floored
// at a minute so a batch arriving at 23:59:59 is not told to come back
// immediately and hammer the boundary.
func SecondsUntilNextDay(now time.Time) int {
	next := UsageDay(now).Add(24 * time.Hour)
	secs := int(next.Sub(now.UTC()).Seconds())
	if secs < 60 {
		return 60
	}
	return secs
}

// WouldExceedCap reports whether taking this batch would put the project past
// its allowance for the day. A cap of zero means uncapped.
//
// All or nothing: a batch that does not fit is refused whole. The published
// SDK deletes every log in a batch on any 200 and has no field in which the
// server could say "I took 300 of your 400", so truncating to the remaining
// allowance would silently destroy the rest on the device.
func WouldExceedCap(stored, incoming, cap int64) bool {
	if cap <= 0 {
		return false
	}
	return stored+incoming > cap
}
