package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
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
	if err := repo.CreateBatch(ctx, logs); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := repo.GetOverview(ctx, projectID)
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
	if err := repo.CreateBatch(ctx, logs); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := repo.GetOverview(ctx, projectID)
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
	if err := repo.CreateBatch(ctx, logs); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := repo.GetOverview(ctx, projectID)
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
