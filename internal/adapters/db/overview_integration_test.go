package db

import (
	"context"
	"testing"
	"time"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/google/uuid"
)

// overviewLog builds one row at an exact time, so a test can say precisely
// which hour it expects a count to land in.
func overviewLog(projectID, sessionID uuid.UUID, level string, at time.Time, network bool, status *int) domain.Log {
	clientID := uuid.New()
	return domain.Log{
		ClientID:      &clientID,
		ProjectID:     projectID,
		SessionID:     sessionID,
		Level:         level,
		Message:       "overview",
		TimeStamp:     at,
		IsNetworkCall: network,
		StatusCode:    status,
	}
}

// The bug this pins: bucket keys were built with time.Truncate on a local
// clock. time.Truncate works on absolute time, so in a +05:30 zone it lands on
// :30 while Postgres date_trunc lands on :00, and every lookup missed. The
// totals still looked plausible, which is what made it dangerous — only the
// per-hour chart was wrong.
//
// Runs the assertion under a deliberately half-hour-offset zone, because on a
// UTC machine the broken version passes.
func TestGetOverviewBucketsLineUpInAHalfHourZone(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)

	// India is +05:30 — the offset that exposed this.
	kolkata := time.FixedZone("IST", int((5*time.Hour + 30*time.Minute).Seconds()))
	original := time.Local
	time.Local = kolkata
	t.Cleanup(func() { time.Local = original })

	// Three hours back, comfortably inside the 24-hour window and away from its
	// edges so a slow test run cannot push it out.
	target := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour).Add(10 * time.Minute)
	session := uuid.New()
	logs := []domain.Log{
		overviewLog(projectID, session, "info", target, false, nil),
		overviewLog(projectID, session, "info", target.Add(time.Minute), false, nil),
		overviewLog(projectID, session, "error", target.Add(2*time.Minute), false, nil),
	}
	if _, err := repo.CreateBatch(ctx, logs); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := repo.GetOverview(ctx, projectID, domain.DefaultOverviewWindow)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}

	if len(got.Buckets) != 24 {
		t.Fatalf("want 24 hourly buckets, got %d", len(got.Buckets))
	}
	if got.Logs != 3 || got.Errors != 1 {
		t.Fatalf("totals wrong: logs=%d errors=%d, want 3 and 1", got.Logs, got.Errors)
	}

	// The counts must sit in the bucket for the hour they happened, not be
	// spread or dropped. This is the assertion the broken version failed.
	wantHour := target.Truncate(time.Hour)
	var found *domain.LogStatsBucket
	for i := range got.Buckets {
		if got.Buckets[i].Hour.UTC().Equal(wantHour) {
			found = &got.Buckets[i]
		}
	}
	if found == nil {
		t.Fatalf("no bucket for %s; buckets run %s..%s",
			wantHour, got.Buckets[0].Hour.UTC(), got.Buckets[23].Hour.UTC())
	}
	if found.TotalCount != 3 || found.ErrorCount != 1 {
		t.Errorf("bucket %s has total=%d errors=%d, want 3 and 1",
			wantHour, found.TotalCount, found.ErrorCount)
	}

	// The sum of the buckets has to equal the headline number, or the chart and
	// the tile above it would disagree.
	var summed int64
	for _, b := range got.Buckets {
		summed += b.TotalCount
	}
	if summed != got.Logs {
		t.Errorf("buckets sum to %d but the total says %d", summed, got.Logs)
	}
}

// Sessions are counted distinctly, which is why they cannot come from the
// hourly rollup: one session logging across three hours is still one session.
func TestGetOverviewCountsSessionsDistinctly(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)

	base := time.Now().UTC().Truncate(time.Hour).Add(-4 * time.Hour)
	session := uuid.New()
	other := uuid.New()
	logs := []domain.Log{
		overviewLog(projectID, session, "info", base.Add(5*time.Minute), false, nil),
		overviewLog(projectID, session, "info", base.Add(time.Hour+5*time.Minute), false, nil),
		overviewLog(projectID, session, "info", base.Add(2*time.Hour+5*time.Minute), false, nil),
		overviewLog(projectID, other, "info", base.Add(10*time.Minute), false, nil),
	}
	if _, err := repo.CreateBatch(ctx, logs); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := repo.GetOverview(ctx, projectID, domain.DefaultOverviewWindow)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if got.Sessions != 2 {
		t.Errorf("want 2 distinct sessions, got %d", got.Sessions)
	}
	if got.Logs != 4 {
		t.Errorf("want 4 logs, got %d", got.Logs)
	}
}

// Network calls and failures are separate counts on the same rows: a failed
// call is still a call, and must not be double counted or missed.
func TestGetOverviewCountsNetworkAndFailures(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)

	at := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour).Add(time.Minute)
	session := uuid.New()
	ok, bad := 200, 500
	logs := []domain.Log{
		overviewLog(projectID, session, "info", at, true, &ok),
		overviewLog(projectID, session, "info", at.Add(time.Minute), true, &bad),
		overviewLog(projectID, session, "info", at.Add(2*time.Minute), false, nil),
	}
	if _, err := repo.CreateBatch(ctx, logs); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := repo.GetOverview(ctx, projectID, domain.DefaultOverviewWindow)
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if got.Network != 2 {
		t.Errorf("want 2 network calls, got %d", got.Network)
	}
	if got.Failed != 1 {
		t.Errorf("want 1 failed call, got %d", got.Failed)
	}
}

// A week's chart folds six hourly rows into each column, and a month's folds
// twenty-four. That folding happens in Go rather than SQL, because 6-hourly has
// no date_trunc and the alternative was an epoch division nobody could read.
// This is the test that says the arithmetic is right.
func TestGetOverviewFoldsHoursIntoTheWindowsBuckets(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)
	session := uuid.New()

	// One log an hour for twelve consecutive hours, ending three hours ago so
	// nothing sits on the window's leading edge.
	base := time.Now().UTC().Truncate(time.Hour).Add(-15 * time.Hour)
	var logs []domain.Log
	for i := 0; i < 12; i++ {
		logs = append(logs, overviewLog(projectID, session, "info", base.Add(time.Duration(i)*time.Hour).Add(5*time.Minute), false, nil))
	}
	if _, err := repo.CreateBatch(ctx, logs); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for _, w := range domain.OverviewWindows {
		got, err := repo.GetOverview(ctx, projectID, w)
		if err != nil {
			t.Fatalf("%s: %v", w.Key, err)
		}

		// Every window is long enough to contain all twelve, so the total is
		// the same however they are bucketed. A fold that dropped or
		// double-counted a row would show up here first.
		if got.Logs != 12 {
			t.Errorf("%s: counted %d logs, want 12", w.Key, got.Logs)
		}
		if len(got.Buckets) != w.Columns() {
			t.Errorf("%s: drew %d columns, want %d", w.Key, len(got.Buckets), w.Columns())
		}

		// And the columns sum to the total, which is what the chart draws.
		var summed int64
		for _, b := range got.Buckets {
			summed += b.TotalCount
		}
		if summed != got.Logs {
			t.Errorf("%s: columns sum to %d but the tile says %d", w.Key, summed, got.Logs)
		}

		// Buckets are contiguous and exactly one Bucket apart, or the axis
		// labels are lying about what each column covers.
		for i := 1; i < len(got.Buckets); i++ {
			if gap := got.Buckets[i].Hour.Sub(got.Buckets[i-1].Hour); gap != w.Bucket {
				t.Fatalf("%s: columns %d and %d are %s apart, want %s", w.Key, i-1, i, gap, w.Bucket)
			}
		}
	}
}

// Twelve hourly logs land in twelve separate columns at hourly buckets, but in
// two or three at six-hourly. If the fold were a no-op the counts would be
// identical across windows and the test above would still pass.
func TestABiggerBucketHoldsMoreOfTheSameLogs(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)
	session := uuid.New()

	base := time.Now().UTC().Truncate(time.Hour).Add(-15 * time.Hour)
	var logs []domain.Log
	for i := 0; i < 12; i++ {
		logs = append(logs, overviewLog(projectID, session, "info", base.Add(time.Duration(i)*time.Hour).Add(5*time.Minute), false, nil))
	}
	if _, err := repo.CreateBatch(ctx, logs); err != nil {
		t.Fatalf("seed: %v", err)
	}

	occupied := func(w domain.OverviewWindow) int {
		got, err := repo.GetOverview(ctx, projectID, w)
		if err != nil {
			t.Fatalf("%s: %v", w.Key, err)
		}
		var n int
		for _, b := range got.Buckets {
			if b.TotalCount > 0 {
				n++
			}
		}
		return n
	}

	day := occupied(domain.OverviewWindowFor("24h", 60))
	week := occupied(domain.OverviewWindowFor("7d", 60))
	month := occupied(domain.OverviewWindowFor("30d", 60))

	if day != 12 {
		t.Errorf("hourly buckets filled %d columns, want 12", day)
	}
	if week >= day {
		t.Errorf("six-hourly filled %d columns and hourly filled %d: the fold did nothing", week, day)
	}
	if month > 2 {
		t.Errorf("daily buckets filled %d columns for 12 hours of logs, want 1 or 2", month)
	}
}

// Whether a project has ever reported is a different question from whether it
// reported in this window, and the screen says opposite things for the two.
func TestEverLoggedSurvivesAnEmptyWindow(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)

	// Nothing at all yet.
	got, err := repo.GetOverview(ctx, projectID, domain.DefaultOverviewWindow)
	if err != nil {
		t.Fatalf("empty project: %v", err)
	}
	if got.EverLogged {
		t.Error("a project with no logs reported EverLogged")
	}

	// One log, well outside the 24 hour window but inside the month.
	old := time.Now().UTC().Add(-10 * 24 * time.Hour)
	if _, err := repo.CreateBatch(ctx, []domain.Log{
		overviewLog(projectID, uuid.New(), "info", old, false, nil),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err = repo.GetOverview(ctx, projectID, domain.DefaultOverviewWindow)
	if err != nil {
		t.Fatalf("quiet window: %v", err)
	}
	if got.Logs != 0 {
		t.Errorf("the 24 hour window counted %d logs from ten days ago", got.Logs)
	}
	if !got.EverLogged {
		t.Error("a project with a ten day old log was reported as never having logged")
	}
}
