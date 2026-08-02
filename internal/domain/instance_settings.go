package domain

import "time"

// InstanceSettings is runtime configuration that lives in the database and is
// editable from the UI. Boot configuration — database host, credentials, port,
// TLS — deliberately stays in file/env and is not reachable from here: a
// setting that can lock you out of the instance does not belong in the
// instance.
//
// One row, not a key/value table. Settings are few and typed, and a column is
// cheaper to add than a parse-and-validate step on every read.
type InstanceSettings struct {
	// Timezone is an IANA name such as "Asia/Kolkata". Every timestamp the
	// dashboard renders is formatted in it, so a team reads times in their own
	// day rather than the server's.
	Timezone string

	// RetentionDays is how long a log lives before the nightly job soft-deletes
	// it. PurgeAfterDays is the grace period after that before the row is gone
	// for good — the two-phase design is what makes an accidental retention
	// change recoverable.
	RetentionDays  int
	PurgeAfterDays int

	// MaxUploadBytes bounds the compressed request body an SDK may send. It is
	// the only unit that bounds socket time and disk spill, because all of that
	// is paid before anything is decompressed.
	MaxUploadBytes int64

	// DailyLogCap is how many logs one project may store per UTC day. Zero
	// means uncapped, and is the default: every already-published SDK treats a
	// 429 as a generic failure and stops syncing after five in a row, so an
	// instance must not start refusing uploads until its clients understand
	// why.
	DailyLogCap int64
}

const (
	// DefaultTimezone is what a fresh instance runs on until someone chooses.
	// UTC rather than the host's zone, so two servers in different regions
	// render the same log the same way.
	DefaultTimezone = "UTC"

	// The retention and upload defaults reproduce exactly what was hardcoded
	// before these became settings, so the day the columns land nothing
	// changes behaviour.
	DefaultRetentionDays  = 30
	DefaultPurgeAfterDays = 7
	DefaultMaxUploadBytes = 50 << 20
	DefaultDailyLogCap    = 0

	// A cap below this is almost certainly a typo, and would refuse the very
	// first batch an SDK sends.
	MinDailyLogCap = 1000

	// Bounds. Retention of 0 is not "keep forever" — it makes the cutoff
	// "now" and soft-deletes every log in the instance on the next run, so the
	// floor is 1 and the form refuses 0 outright.
	MinRetentionDays  = 1
	MaxRetentionDays  = 3650
	MinPurgeAfterDays = 1
	MaxPurgeAfterDays = 365

	// The upload ceiling is capped in the domain, not just hinted at in the
	// form. Boot configuration is deliberately unreachable from the UI, and an
	// unbounded upload size would put the gzip-bomb margin in the same place.
	MinMaxUploadMB = 1
	MaxMaxUploadMB = 512
)

// DefaultInstanceSettings is the whole struct a fresh instance runs on. Written
// once here rather than repeated in the service constructor and the repository
// fallback, which is how two of them drift apart.
func DefaultInstanceSettings() InstanceSettings {
	return InstanceSettings{
		Timezone:       DefaultTimezone,
		RetentionDays:  DefaultRetentionDays,
		PurgeAfterDays: DefaultPurgeAfterDays,
		MaxUploadBytes: DefaultMaxUploadBytes,
		DailyLogCap:    DefaultDailyLogCap,
	}
}

// MaxUploadMB is the upload cap in the unit a person actually types. Nobody
// enters 52428800.
func (s InstanceSettings) MaxUploadMB() int { return int(s.MaxUploadBytes >> 20) }

func ValidRetentionDays(days int) bool {
	return days >= MinRetentionDays && days <= MaxRetentionDays
}

func ValidPurgeAfterDays(days int) bool {
	return days >= MinPurgeAfterDays && days <= MaxPurgeAfterDays
}

func ValidMaxUploadMB(mb int) bool {
	return mb >= MinMaxUploadMB && mb <= MaxMaxUploadMB
}

// ValidDailyLogCap accepts zero, which is how "no cap" is expressed.
func ValidDailyLogCap(cap int64) bool {
	return cap == 0 || cap >= MinDailyLogCap
}

// Location resolves the configured zone, falling back to UTC. A stored name
// that the host has no tzdata for must not break every page that shows a time.
func (s InstanceSettings) Location() *time.Location {
	if s.Timezone == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// ValidTimezone reports whether a name can actually be loaded here. Checked
// before storing, so an unusable zone is refused at the form rather than
// silently degrading to UTC on every later read.
func ValidTimezone(name string) bool {
	if name == "" {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}
