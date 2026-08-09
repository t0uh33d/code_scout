package view

import (
	"strings"
	"testing"
	"time"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/google/uuid"
)

func overviewFor(window domain.OverviewWindow, everLogged bool, logs int64) OverviewData {
	start := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	stats := &domain.ProjectOverview{
		Logs:            logs,
		EverLogged:      everLogged,
		Window:          window,
		WindowStartHour: start,
	}
	for i := 0; i < window.Columns(); i++ {
		stats.Buckets = append(stats.Buckets, domain.LogStatsBucket{
			Hour: start.Add(time.Duration(i) * window.Bucket),
		})
	}
	if logs > 0 {
		stats.Buckets[0].TotalCount = logs
	}
	return OverviewData{
		User:      &domain.User{Name: "T", Email: "t@test.local"},
		Project:   &domain.Project{},
		ProjectID: uuid.New(),
		Stats:     stats,
		Window:    window,
		Windows:   domain.OverviewWindowsFor(30),
		ShowDelta: window.ComparableAgainstPrevious(30),
	}
}

// The bug this screen had: the empty state was gated on the count in the
// window, so a project with a year of history that was quiet overnight was told
// it had never reported and lost its tiles, its chart and its errors to say so.
func TestAQuietWindowIsNotAProjectThatNeverReported(t *testing.T) {
	d := overviewFor(domain.OverviewWindowFor("7d", 30), true, 0)
	html := render(t, OverviewPage(d))

	if strings.Contains(html, "Nothing has reported yet") {
		t.Error("a project that has logged before was told it never has")
	}
	if strings.Contains(html, "Show me the snippet") {
		t.Error("a working project was sent to the SDK setup page")
	}
	// It still says the window was quiet, and offers the way out.
	if !strings.Contains(html, "Nothing was logged in last 7 days") {
		t.Error("the screen did not say the window itself was empty")
	}
	if !strings.Contains(html, "Try a longer range") {
		t.Error("no suggestion of what to do about an empty window")
	}
	// And the tiles are still there, at zero.
	if !strings.Contains(html, "Total logs") {
		t.Error("the tiles went away")
	}
}

// The genuine first run still gets the first-run screen, or the test above
// would pass with the fix removed.
func TestAProjectThatHasNeverReportedStillGetsTheSnippet(t *testing.T) {
	d := overviewFor(domain.DefaultOverviewWindow, false, 0)
	html := render(t, OverviewPage(d))

	if !strings.Contains(html, "Nothing has reported yet") {
		t.Error("a brand new project did not get the first-run state")
	}
	if !strings.Contains(html, "Show me the snippet") {
		t.Error("the first-run state lost its call to action")
	}
	// No range picker on a project with nothing to range over.
	if strings.Contains(html, `data-range=`) {
		t.Error("a project with no data was offered a time range")
	}
}

// The range is a link, so the view is shareable and the back button works.
func TestTheRangeIsALinkCarryingItsStateInTheURL(t *testing.T) {
	d := overviewFor(domain.OverviewWindowFor("7d", 30), true, 120)
	html := render(t, OverviewPage(d))

	for _, want := range []string{`data-range="24h"`, `data-range="7d"`, `data-range="30d"`, "?range=7d"} {
		if !strings.Contains(html, want) {
			t.Errorf("the range picker is missing %s", want)
		}
	}
	if !strings.Contains(html, `data-range="7d" aria-current="page"`) {
		t.Error("the active range is not marked current")
	}
}

// Retention decides what is honest to offer. A seven day instance must not
// offer a month, because the chart would draw a cliff and then flat ground and
// nothing on screen would say why.
func TestARangeRetentionCannotFillIsNotOffered(t *testing.T) {
	d := overviewFor(domain.DefaultOverviewWindow, true, 40)
	d.Windows = domain.OverviewWindowsFor(7)
	html := render(t, OverviewPage(d))

	if strings.Contains(html, `data-range="30d"`) {
		t.Error("a range past retention was offered")
	}
	if !strings.Contains(html, `data-range="7d"`) {
		t.Error("a range inside retention was dropped")
	}
}

// The labels move with the range. "vs yesterday" on a month of data is wrong,
// and so is "worst hour" when a column is a day.
func TestTheLabelsFollowTheRange(t *testing.T) {
	week := overviewFor(domain.OverviewWindowFor("7d", 30), true, 500)
	week.Stats.PeakErrorCount = 3
	week.Stats.PeakErrorHour = week.Stats.WindowStartHour
	html := render(t, OverviewPage(week))

	if !strings.Contains(html, "vs the previous 7 days") {
		t.Error("the delta still compares against yesterday on a week")
	}
	if strings.Contains(html, "vs yesterday") {
		t.Error("a week-long window claimed a day-over-day delta")
	}
	if !strings.Contains(html, "Activity · last 7 days") {
		t.Error("the chart heading is still hardcoded to 24 hours")
	}
	if !strings.Contains(html, "worst hour for errors") {
		t.Error("the callout lost its noun")
	}

	month := overviewFor(domain.OverviewWindowFor("30d", 60), true, 900)
	month.Stats.PeakErrorCount = 9
	month.Stats.PeakErrorHour = month.Stats.WindowStartHour
	if got := render(t, OverviewPage(month)); !strings.Contains(got, "worst day for errors") {
		t.Error("a daily bucket is still called an hour")
	}
}

// The window fitting inside retention is not enough: the delta reaches back
// another whole window. Printing a rise against a month that was deleted is
// worse than printing no delta at all.
func TestNoDeltaIsShownWhenThePreviousPeriodIsGone(t *testing.T) {
	d := overviewFor(domain.OverviewWindowFor("30d", 30), true, 5000)
	d.ShowDelta = false
	html := render(t, OverviewPage(d))

	if strings.Contains(html, "vs the previous 30 days") {
		t.Error("a delta was shown against a period retention has deleted")
	}
	// The counts themselves are still there.
	if !strings.Contains(html, "Total logs") {
		t.Error("hiding the delta took the tile with it")
	}
}
