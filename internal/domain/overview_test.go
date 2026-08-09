package domain

import (
	"testing"
	"time"
)

// The chart keeps its shape across ranges. That is the whole reason the bucket
// grows with the window: a week at hourly buckets is 168 columns, which is a
// grey smear at the width this chart has.
func TestEveryWindowDrawsAboutTheSameNumberOfColumns(t *testing.T) {
	for _, w := range OverviewWindows {
		cols := w.Columns()
		if cols < 24 || cols > 31 {
			t.Errorf("%s draws %d columns, want roughly 24 to 30", w.Key, cols)
		}
		if w.Span != time.Duration(w.Days)*24*time.Hour {
			t.Errorf("%s: Days (%d) and Span (%s) disagree", w.Key, w.Days, w.Span)
		}
	}
}

// A window longer than retention is a chart of rows the nightly job deleted. It
// draws a cliff and then flat ground, which reads as a collapse in traffic
// rather than as the edge of what we keep.
func TestRetentionTakesAwayRangesItCannotFill(t *testing.T) {
	keys := func(ws []OverviewWindow) []string {
		out := make([]string, 0, len(ws))
		for _, w := range ws {
			out = append(out, w.Key)
		}
		return out
	}

	cases := map[int][]string{
		30: {"24h", "7d", "30d"},
		29: {"24h", "7d"},
		7:  {"24h", "7d"},
		6:  {"24h"},
		1:  {"24h"},
	}
	for keep, want := range cases {
		got := keys(OverviewWindowsFor(keep))
		if len(got) != len(want) {
			t.Errorf("retention %d offers %v, want %v", keep, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("retention %d offers %v, want %v", keep, got, want)
				break
			}
		}
	}
}

// A screen with no ranges at all has no control left to get back to a working
// one, so the shortest survives any setting however absurd.
func TestTheShortestRangeIsAlwaysOffered(t *testing.T) {
	for _, keep := range []int{0, -1} {
		got := OverviewWindowsFor(keep)
		if len(got) != 1 || got[0].Key != "24h" {
			t.Errorf("retention %d offered %d ranges, want just 24h", keep, len(got))
		}
	}
}

// The key arrives from a query string, so anything can be in it.
func TestAnUnknownOrUnavailableRangeFallsBackRatherThanFailing(t *testing.T) {
	if got := OverviewWindowFor("nonsense", 30); got.Key != DefaultOverviewWindow.Key {
		t.Errorf("an unknown key gave %q", got.Key)
	}
	if got := OverviewWindowFor("", 30); got.Key != DefaultOverviewWindow.Key {
		t.Errorf("an empty key gave %q", got.Key)
	}
	// Valid, but this instance cannot fill it.
	if got := OverviewWindowFor("30d", 7); got.Key != "24h" {
		t.Errorf("a range past retention gave %q, want the default", got.Key)
	}
	if got := OverviewWindowFor("30d", 30); got.Key != "30d" {
		t.Errorf("a range inside retention was refused, gave %q", got.Key)
	}
}

// The delta reaches back a whole window further than the window itself. On the
// default 30 day retention, "Last 30 days" would otherwise report every log as
// a rise over a month that no longer exists.
func TestTheDeltaIsOnlyOfferedWhenThePreviousPeriodSurvives(t *testing.T) {
	cases := []struct {
		key  string
		keep int
		want bool
	}{
		{"24h", 30, true},
		{"7d", 30, true},
		{"30d", 30, false}, // the window fits, the comparison does not
		{"30d", 60, true},
		{"7d", 14, true},
		{"7d", 13, false},
		{"24h", 1, false},
		{"24h", 2, true},
	}
	for _, c := range cases {
		w := OverviewWindowFor(c.key, 60) // resolve the window itself unclamped
		if got := w.ComparableAgainstPrevious(c.keep); got != c.want {
			t.Errorf("%s at %d days retention: comparable = %v, want %v", c.key, c.keep, got, c.want)
		}
	}
}

// The callout under the chart names one column, and a column is not always an
// hour.
func TestBucketNounFollowsTheBucket(t *testing.T) {
	for _, c := range []struct{ key, want string }{
		{"24h", "hour"}, {"7d", "hour"}, {"30d", "day"},
	} {
		if got := OverviewWindowFor(c.key, 60).BucketNoun(); got != c.want {
			t.Errorf("%s calls a column a %q, want %q", c.key, got, c.want)
		}
	}
}
