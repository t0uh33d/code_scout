package domain

import "time"

// OverviewWindow is how far back the overview looks, and how it buckets what it
// finds.
//
// Bucket is picked so every window draws a similar number of columns: 24, 28,
// 30. The chart keeps its shape whichever range is chosen and only the axis
// labels change, rather than going from 24 fat bars at a day to 168 hairlines
// at a week.
type OverviewWindow struct {
	// Key is what travels in the query string.
	Key   string
	Label string

	// Span is how far back the window reaches. Bucket is one column.
	Span   time.Duration
	Bucket time.Duration

	// PrevLabel is what the tile deltas are measured against, which moves with
	// the window: comparing a week against yesterday would be nonsense.
	PrevLabel string

	// Days is Span in whole days, for the retention comparison.
	Days int
}

// Columns is how many bars the chart draws.
func (w OverviewWindow) Columns() int {
	if w.Bucket <= 0 {
		return 0
	}
	return int(w.Span / w.Bucket)
}

// BucketNoun names one column, for the "worst hour" / "worst day" callout.
func (w OverviewWindow) BucketNoun() string {
	if w.Bucket >= 24*time.Hour {
		return "day"
	}
	return "hour"
}

// OverviewWindows are the ranges the overview offers, shortest first.
var OverviewWindows = []OverviewWindow{
	{Key: "24h", Label: "Last 24 hours", Span: 24 * time.Hour, Bucket: time.Hour,
		PrevLabel: "vs yesterday", Days: 1},
	{Key: "7d", Label: "Last 7 days", Span: 7 * 24 * time.Hour, Bucket: 6 * time.Hour,
		PrevLabel: "vs the previous 7 days", Days: 7},
	{Key: "30d", Label: "Last 30 days", Span: 30 * 24 * time.Hour, Bucket: 24 * time.Hour,
		PrevLabel: "vs the previous 30 days", Days: 30},
}

// DefaultOverviewWindow is what you land on.
var DefaultOverviewWindow = OverviewWindows[0]

// OverviewWindowsFor is the set worth offering on an instance that keeps logs
// for keepDays.
//
// A window longer than retention is not a longer view, it is a chart of rows
// the nightly job already deleted. It draws a cliff and then flat ground, which
// reads as "our traffic collapsed" rather than "we do not keep that far back",
// and there is nothing on the screen to say which. So the option is not offered
// at all.
//
// The shortest window is always offered even on an absurd setting, because a
// screen with no ranges at all has no way back to a working one.
func OverviewWindowsFor(keepDays int) []OverviewWindow {
	out := make([]OverviewWindow, 0, len(OverviewWindows))
	for _, w := range OverviewWindows {
		if w.Days <= keepDays {
			out = append(out, w)
		}
	}
	if len(out) == 0 {
		return []OverviewWindow{OverviewWindows[0]}
	}
	return out
}

// OverviewWindowFor resolves a key against what this instance can honestly
// show. An unknown key, or one retention has taken away, falls back to the
// default rather than erroring: it arrives from a query string.
func OverviewWindowFor(key string, keepDays int) OverviewWindow {
	for _, w := range OverviewWindowsFor(keepDays) {
		if w.Key == key {
			return w
		}
	}
	return DefaultOverviewWindow
}

// ComparableAgainstPrevious reports whether the delta on the tiles means
// anything.
//
// The window itself fitting inside retention is not enough: the delta compares
// against the period *before* it, so that period has to still exist too. On the
// default 30 day retention, "Last 30 days" would otherwise report every log as
// an increase over a previous month that was deleted. When this is false the
// tiles show the count and no delta, which is the honest version of not
// knowing.
func (w OverviewWindow) ComparableAgainstPrevious(keepDays int) bool {
	return w.Days*2 <= keepDays
}
