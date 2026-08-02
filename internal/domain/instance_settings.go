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
}

// DefaultTimezone is what a fresh instance runs on until someone chooses.
// UTC rather than the host's zone, so two servers in different regions render
// the same log the same way.
const DefaultTimezone = "UTC"

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
