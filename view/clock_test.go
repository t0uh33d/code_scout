package view

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/getcodescout/code_scout/internal/domain"
)

// The point of the setting is that one choice moves every timestamp, so the
// thing worth pinning is that no format is left behind.
func TestEveryTimeFormatHonoursTheClock(t *testing.T) {
	t.Cleanup(func() { SetTwelveHourClock(false) })

	// Afternoon on purpose: a morning time renders the same in both clocks
	// apart from the suffix, so it would not catch a layout still using 15.
	at := time.Date(2026, 8, 4, 15, 4, 5, 250_000_000, time.UTC)
	SetTimeZone(time.UTC)

	carriesTime := []TimeFormat{TimeMillis, TimeSeconds, TimeShort, DateTimeShort, DateTimeMillis, DateTimeWithDay}

	SetTwelveHourClock(false)
	for _, f := range carriesTime {
		got := fmtTime(at, f)
		if !strings.Contains(got, "15:04") {
			t.Errorf("format %d should render 24-hour, got %q", f, got)
		}
		if strings.Contains(got, "PM") {
			t.Errorf("format %d leaked a meridiem into the 24-hour clock: %q", f, got)
		}
	}

	SetTwelveHourClock(true)
	for _, f := range carriesTime {
		got := fmtTime(at, f)
		if !strings.HasSuffix(got, "PM") {
			t.Errorf("format %d should end in a meridiem, got %q", f, got)
		}
		if strings.Contains(got, "15:04") {
			t.Errorf("format %d still renders a 24-hour clock: %q", f, got)
		}
		if !strings.Contains(got, "3:04") {
			t.Errorf("format %d should render the 12-hour equivalent, got %q", f, got)
		}
	}
}

// A date with no time in it has nothing to switch, and must not grow a "PM".
func TestDateOnlyFormatsIgnoreTheClock(t *testing.T) {
	t.Cleanup(func() { SetTwelveHourClock(false) })

	at := time.Date(2026, 8, 4, 15, 4, 5, 0, time.UTC)
	SetTimeZone(time.UTC)

	for _, f := range []TimeFormat{DateShort, DateLong} {
		SetTwelveHourClock(false)
		in24 := fmtTime(at, f)
		SetTwelveHourClock(true)
		in12 := fmtTime(at, f)

		if in24 != in12 {
			t.Errorf("a date-only format changed with the clock: %q then %q", in24, in12)
		}
		if strings.Contains(in12, "PM") {
			t.Errorf("a date-only format grew a meridiem: %q", in12)
		}
	}
}

// The zone and the clock are separate settings, and have to stay separable.
func TestTheClockDoesNotDisturbTheZone(t *testing.T) {
	t.Cleanup(func() {
		SetTwelveHourClock(false)
		SetTimeZone(time.UTC)
	})

	kolkata, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Skip("no tzdata for Asia/Kolkata on this host")
	}

	// 09:30 UTC is 15:00 in Kolkata, which is afternoon in one clock and not
	// the other — so this fails if either setting is applied without the other.
	at := time.Date(2026, 8, 4, 9, 30, 0, 0, time.UTC)
	SetTimeZone(kolkata)

	SetTwelveHourClock(false)
	if got := fmtTime(at, TimeShort); got != "15:00" {
		t.Errorf("want 15:00 in Kolkata, got %q", got)
	}
	SetTwelveHourClock(true)
	if got := fmtTime(at, TimeShort); got != "3:00 PM" {
		t.Errorf("want 3:00 PM in Kolkata, got %q", got)
	}
}

// An unnamed format is a programming error. Rendering a blank where a timestamp
// belongs turns it into a support question instead.
func TestAnUnknownFormatStillRendersATime(t *testing.T) {
	t.Cleanup(func() { SetTwelveHourClock(false) })
	SetTimeZone(time.UTC)
	SetTwelveHourClock(false)

	if got := fmtTime(time.Date(2026, 8, 4, 15, 4, 5, 0, time.UTC), TimeFormat(99)); got == "" {
		t.Error("an unknown format rendered nothing")
	}
}

// The 12-hour column is wider, and not a fixed width either: "1:14:25 AM"
// against "12:36:33 AM". Pinned at the 24-hour width the longer ones wrapped
// onto a second line and those rows stood taller than the rest of the list.
func TestTheLogRowWidensForATwelveHourClock(t *testing.T) {
	t.Cleanup(func() { SetTwelveHourClock(false) })
	SetTimeZone(time.UTC)

	logs := &domain.LogListResult{Items: []domain.Log{{
		ID:        uuid.New(),
		Level:     "error",
		Message:   "Payment declined",
		TimeStamp: time.Date(2026, 8, 4, 0, 36, 33, 0, time.UTC),
	}}}
	render := func() string {
		var b strings.Builder
		if err := LogRows(logs, uuid.New(), "").Render(context.Background(), &b); err != nil {
			t.Fatalf("render: %v", err)
		}
		return b.String()
	}

	SetTwelveHourClock(false)
	in24 := render()
	if !strings.Contains(in24, "grid-cols-[70px_68px_1fr_auto]") {
		t.Errorf("the 24-hour row lost its column width: %s", in24)
	}

	SetTwelveHourClock(true)
	in12 := render()
	if !strings.Contains(in12, "grid-cols-[96px_68px_1fr_auto]") {
		t.Errorf("the 12-hour row did not widen its time column: %s", in12)
	}
	if !strings.Contains(in12, "whitespace-nowrap") {
		t.Error("the timestamp may still wrap")
	}
	if !strings.Contains(in12, "12:36:33 AM") {
		t.Errorf("expected a 12-hour timestamp, got: %s", in12)
	}
}

