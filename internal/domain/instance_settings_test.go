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

// The defaults must reproduce exactly what was hardcoded before these became
// settings, or the day the columns land quietly changes how long logs live and
// how large an upload may be.
func TestDefaultsMatchWhatWasHardcoded(t *testing.T) {
	d := DefaultInstanceSettings()

	if d.RetentionDays != 30 {
		t.Errorf("retention was 30 days in main.go, got %d", d.RetentionDays)
	}
	if d.PurgeAfterDays != 7 {
		t.Errorf("purge-after was 7 days in main.go, got %d", d.PurgeAfterDays)
	}
	if d.MaxUploadBytes != 50<<20 {
		t.Errorf("the upload cap was 50 MB in log_handler.go, got %d", d.MaxUploadBytes)
	}
	if d.Timezone != DefaultTimezone {
		t.Errorf("want %s, got %s", DefaultTimezone, d.Timezone)
	}
}

// Zero is the dangerous value: it puts the retention cutoff at "now", so the
// next nightly run soft-deletes every log in the instance. It must be refused
// rather than read as "keep forever".
func TestRetentionRefusesZero(t *testing.T) {
	if ValidRetentionDays(0) {
		t.Error("0 days would soft-delete everything on the next run")
	}
	if ValidPurgeAfterDays(0) {
		t.Error("0 days destroys the grace period the two-phase design exists for")
	}
	if ValidRetentionDays(-1) {
		t.Error("a negative window puts the cutoff in the future")
	}
}

func TestSettingBounds(t *testing.T) {
	cases := []struct {
		name  string
		valid bool
		got   bool
	}{
		{"retention at the floor", true, ValidRetentionDays(MinRetentionDays)},
		{"retention at the ceiling", true, ValidRetentionDays(MaxRetentionDays)},
		{"retention over the ceiling", false, ValidRetentionDays(MaxRetentionDays + 1)},
		{"purge at the floor", true, ValidPurgeAfterDays(MinPurgeAfterDays)},
		{"purge over the ceiling", false, ValidPurgeAfterDays(MaxPurgeAfterDays + 1)},
		{"upload at the floor", true, ValidMaxUploadMB(MinMaxUploadMB)},
		{"upload at the ceiling", true, ValidMaxUploadMB(MaxMaxUploadMB)},
		// An unbounded upload cap would put the gzip-bomb margin in the hands
		// of whoever can reach the form.
		{"upload over the ceiling", false, ValidMaxUploadMB(MaxMaxUploadMB + 1)},
		{"upload of zero", false, ValidMaxUploadMB(0)},
	}
	for _, c := range cases {
		if c.got != c.valid {
			t.Errorf("%s: want valid=%t, got %t", c.name, c.valid, c.got)
		}
	}
}

func TestMaxUploadMBRoundTrips(t *testing.T) {
	s := InstanceSettings{MaxUploadBytes: 50 << 20}
	if s.MaxUploadMB() != 50 {
		t.Errorf("want 50 MB, got %d", s.MaxUploadMB())
	}
}
