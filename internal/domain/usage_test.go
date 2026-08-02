package domain

import (
	"testing"
	"time"
)

// The bucket is a UTC calendar day, deliberately not the instance timezone:
// that setting is editable, and a movable boundary would retroactively change
// which day existing rows were counted against.
func TestUsageDayIsUTC(t *testing.T) {
	// Late evening in Kolkata is already the next day in UTC.
	kolkata := time.FixedZone("IST", 5*3600+1800)
	at := time.Date(2026, 8, 2, 23, 30, 0, 0, kolkata)

	day := UsageDay(at)
	if day.Location() != time.UTC {
		t.Errorf("the bucket must be in UTC, got %s", day.Location())
	}
	if day.Day() != 2 || day.Month() != time.August {
		t.Errorf("want 2 August UTC, got %s", day.Format("2 Jan"))
	}
	if day.Hour() != 0 || day.Minute() != 0 {
		t.Errorf("the bucket should be midnight, got %s", day)
	}
}

func TestUsageDayCollapsesAMoment(t *testing.T) {
	morning := time.Date(2026, 8, 2, 6, 15, 0, 0, time.UTC)
	evening := time.Date(2026, 8, 2, 22, 45, 0, 0, time.UTC)
	if !UsageDay(morning).Equal(UsageDay(evening)) {
		t.Error("two moments on the same UTC day must share a bucket")
	}
}

func TestSecondsUntilNextDay(t *testing.T) {
	// Six hours to go.
	at := time.Date(2026, 8, 2, 18, 0, 0, 0, time.UTC)
	if got := SecondsUntilNextDay(at); got != 6*3600 {
		t.Errorf("want 21600, got %d", got)
	}
}

// A batch arriving a second before midnight must not be told to retry in one
// second and hammer the boundary.
func TestSecondsUntilNextDayHasAFloor(t *testing.T) {
	at := time.Date(2026, 8, 2, 23, 59, 59, 0, time.UTC)
	if got := SecondsUntilNextDay(at); got < 60 {
		t.Errorf("want at least a minute, got %d", got)
	}
}

func TestWouldExceedCap(t *testing.T) {
	cases := []struct {
		name                   string
		stored, incoming, capN int64
		want                   bool
	}{
		{"uncapped is never exceeded", 1_000_000, 1_000_000, 0, false},
		{"a negative cap is also uncapped", 5, 5, -1, false},
		{"comfortably under", 10, 10, 100, false},
		// Exactly at the cap is allowed; one past it is not.
		{"exactly at the cap", 90, 10, 100, false},
		{"one past the cap", 91, 10, 100, true},
		{"already over", 200, 1, 100, true},
		{"an empty batch never exceeds", 100, 0, 100, false},
	}
	for _, c := range cases {
		if got := WouldExceedCap(c.stored, c.incoming, c.capN); got != c.want {
			t.Errorf("%s: want %t, got %t", c.name, c.want, got)
		}
	}
}
