package view

import (
	"fmt"
	"sync"
	"time"
)

// Every timestamp the dashboard renders goes through here, so changing the
// instance timezone changes all of them at once.
//
// Package-level state rather than a value threaded through each template: the
// timezone is genuinely one instance-wide setting, and passing it into every
// templ that happens to show a time would touch far more code than it is worth
// while still ending up with a single source. The mutex is what makes it safe
// to change while requests are reading.
var (
	zoneMu sync.RWMutex
	zone   = time.UTC
)

// SetTimeZone is called at boot and whenever the setting is saved.
func SetTimeZone(loc *time.Location) {
	if loc == nil {
		loc = time.UTC
	}
	zoneMu.Lock()
	zone = loc
	zoneMu.Unlock()
}

// TimeZone is the zone the dashboard is currently rendering in.
func TimeZone() *time.Location {
	zoneMu.RLock()
	defer zoneMu.RUnlock()
	return zone
}

// inZone moves a timestamp into the configured zone. Postgres hands back
// timestamptz in whatever zone the driver session uses, so the value has to be
// converted at render rather than trusted as-is.
func inZone(t time.Time) time.Time { return t.In(TimeZone()) }

// TimeFormat names a shape rather than a Go layout string.
//
// Named because there are two layouts behind each one, and which gets used is
// an instance setting. Call sites used to pass "15:04:05" and friends directly,
// which left nowhere to put the 12-hour twin except a substitution on the
// layout — and a substitution has to guess where "PM" belongs.
type TimeFormat int

const (
	// TimeMillis is a log's timestamp in the viewer: 15:04:05.000.
	TimeMillis TimeFormat = iota
	// TimeSeconds drops the milliseconds, for timelines and session starts.
	TimeSeconds
	// TimeShort is an hour and minute, used for chart buckets.
	TimeShort
	// DateTimeShort is a day and a time: 2 Jan 15:04.
	DateTimeShort
	// DateTimeMillis is a day and a precise time, for the network inspector.
	DateTimeMillis
	// DateTimeWithDay names the weekday too, for the settings preview where the
	// whole point is to show what the format looks like.
	DateTimeWithDay
	// DateShort is a date with no time at all: 2 Jan 2006.
	DateShort
	// DateLong spells the month out: 2 January 2006.
	DateLong
)

// The layouts, 24-hour first. Every entry that carries a time puts it last,
// which is what lets the 12-hour column simply end in PM.
var timeLayouts = map[TimeFormat][2]string{
	TimeMillis:     {"15:04:05.000", "3:04:05.000 PM"},
	TimeSeconds:    {"15:04:05", "3:04:05 PM"},
	TimeShort:      {"15:04", "3:04 PM"},
	DateTimeShort:  {"2 Jan 15:04", "2 Jan 3:04 PM"},
	DateTimeMillis:  {"2 Jan 15:04:05.000", "2 Jan 3:04:05.000 PM"},
	DateTimeWithDay: {"Mon 2 Jan, 15:04", "Mon 2 Jan, 3:04 PM"},
	DateShort:      {"2 Jan 2006", "2 Jan 2006"},
	DateLong:       {"2 January 2006", "2 January 2006"},
}

// Whether times render as 15:04 or 3:04 PM. Package-level for the same reason
// the zone is: it is one instance-wide setting, and threading it through every
// template that shows a time would touch far more code than it is worth.
var (
	clockMu  sync.RWMutex
	twelveHr bool
)

// SetTwelveHourClock is called at boot and whenever the setting is saved.
func SetTwelveHourClock(on bool) {
	clockMu.Lock()
	twelveHr = on
	clockMu.Unlock()
}

// TwelveHourClock is what the dashboard is currently rendering in.
func TwelveHourClock() bool {
	clockMu.RLock()
	defer clockMu.RUnlock()
	return twelveHr
}

// fmtTime is the one formatter templates call.
func fmtTime(t time.Time, f TimeFormat) string {
	layouts, ok := timeLayouts[f]
	if !ok {
		// An unnamed format is a programming error, not a reason to render a
		// blank where a timestamp belongs.
		layouts = timeLayouts[TimeSeconds]
	}
	if TwelveHourClock() {
		return inZone(t).Format(layouts[1])
	}
	return inZone(t).Format(layouts[0])
}

// shortDuration is how long a session ran: "41s", "6m 41s", "1h 04m". Seconds
// are dropped past an hour, where they stop telling you anything.
func shortDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm %02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

// relTime is "14m ago" — how the Errors screen says when something was first
// and last seen. Coarse on purpose: "3d ago" is what you want to know about a
// bug, and the exact timestamp is one click away in the logs.
//
// Needs no zone: an elapsed duration is the same everywhere.
func relTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
