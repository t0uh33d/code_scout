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

// fmtTime is the one formatter templates call.
func fmtTime(t time.Time, layout string) string { return inZone(t).Format(layout) }

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
