package domain

import (
	"testing"
	"time"
)

func TestLocationFallsBackToUTC(t *testing.T) {
	cases := map[string]string{
		"empty":     "",
		"nonsense":  "Middle/Earth",
		"an offset": "+05:30",
	}
	for name, tz := range cases {
		t.Run(name, func(t *testing.T) {
			// A stored zone this host cannot load must not break every page that
			// shows a time, so it degrades to UTC rather than returning nil.
			if got := (InstanceSettings{Timezone: tz}).Location(); got != time.UTC {
				t.Errorf("want UTC for %q, got %v", tz, got)
			}
		})
	}
}

func TestLocationResolvesARealZone(t *testing.T) {
	settings := InstanceSettings{Timezone: "Asia/Kolkata"}
	loc := settings.Location()
	if loc == time.UTC {
		t.Fatal("Asia/Kolkata resolved to UTC, so the zone was not loaded")
	}

	// +05:30 is the offset that broke the overview buckets, so it is the one
	// worth asserting: a zone that is not a whole number of hours from UTC.
	at := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC).In(loc)
	if got := at.Format("15:04"); got != "17:30" {
		t.Errorf("12:00 UTC should read 17:30 in Kolkata, got %s", got)
	}
}

func TestValidTimezone(t *testing.T) {
	valid := []string{"UTC", "Asia/Kolkata", "America/New_York", "Europe/London"}
	for _, tz := range valid {
		if !ValidTimezone(tz) {
			t.Errorf("%q should be accepted", tz)
		}
	}

	// Refused at the form rather than silently degrading to UTC on every later
	// read, which would look like the setting simply did not save.
	invalid := []string{"", "   ", "Middle/Earth", "+05:30", "IST", "Asia/Kolkata "}
	for _, tz := range invalid {
		if ValidTimezone(tz) {
			t.Errorf("%q should be refused", tz)
		}
	}
}

func TestDefaultTimezoneIsUTC(t *testing.T) {
	// Not the host's zone: two servers in different regions must render the
	// same log the same way until someone chooses otherwise.
	if DefaultTimezone != "UTC" {
		t.Errorf("default should be UTC, got %q", DefaultTimezone)
	}
	if !ValidTimezone(DefaultTimezone) {
		t.Error("the default must itself be loadable")
	}
}
