package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/getcodescout/code_scout/internal/domain"
)

func TestUsageCountsFromZero(t *testing.T) {
	db := testDB(t)
	repo := NewUsageRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)
	t.Cleanup(func() { db.Where("project_id = ?", projectID).Delete(&ProjectUsageDailyModel{}) })

	// A project that has sent nothing today has no row at all, which is the
	// normal state at the start of every day and must not read as an error.
	got, err := repo.CountForDay(ctx, projectID, time.Now())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if got != 0 {
		t.Errorf("want 0, got %d", got)
	}
}

// Add accumulates rather than replacing, and stays one row per project per day.
func TestUsageAccumulatesInOneRow(t *testing.T) {
	db := testDB(t)
	repo := NewUsageRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)
	t.Cleanup(func() { db.Where("project_id = ?", projectID).Delete(&ProjectUsageDailyModel{}) })

	now := time.Now()
	for _, n := range []int64{100, 50, 25} {
		if err := repo.Add(ctx, projectID, now, n); err != nil {
			t.Fatalf("add %d: %v", n, err)
		}
	}

	got, err := repo.CountForDay(ctx, projectID, now)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if got != 175 {
		t.Errorf("want 175, got %d", got)
	}

	var rows int64
	db.Model(&ProjectUsageDailyModel{}).Where("project_id = ?", projectID).Count(&rows)
	if rows != 1 {
		t.Errorf("want one row per project per day, got %d", rows)
	}
}

// Days are separate buckets, and asking about one never returns another's.
func TestUsageSeparatesDays(t *testing.T) {
	db := testDB(t)
	repo := NewUsageRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)
	t.Cleanup(func() { db.Where("project_id = ?", projectID).Delete(&ProjectUsageDailyModel{}) })

	today := time.Now()
	yesterday := today.Add(-24 * time.Hour)

	if err := repo.Add(ctx, projectID, yesterday, 500); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := repo.Add(ctx, projectID, today, 7); err != nil {
		t.Fatalf("add: %v", err)
	}

	if got, _ := repo.CountForDay(ctx, projectID, today); got != 7 {
		t.Errorf("today should be 7, got %d", got)
	}
	if got, _ := repo.CountForDay(ctx, projectID, yesterday); got != 500 {
		t.Errorf("yesterday should be 500, got %d", got)
	}
}

// One project's volume never counts against another's allowance.
func TestUsageIsProjectScoped(t *testing.T) {
	db := testDB(t)
	repo := NewUsageRepo(db)
	ctx := context.Background()
	mine := seedProject(t, db)
	theirs := seedProject(t, db)
	t.Cleanup(func() {
		db.Where("project_id IN ?", []uuid.UUID{mine, theirs}).Delete(&ProjectUsageDailyModel{})
	})

	now := time.Now()
	if err := repo.Add(ctx, mine, now, 10); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := repo.Add(ctx, theirs, now, 9999); err != nil {
		t.Fatalf("add: %v", err)
	}

	if got, _ := repo.CountForDay(ctx, mine, now); got != 10 {
		t.Errorf("another project's volume leaked in: %d", got)
	}
}

// The load-bearing property. A replayed batch after a lost 200 inserts nothing,
// so the counter must move by the rows actually written — otherwise a flaky
// network burns a project's whole day on rows it already has.
func TestUsageIncrementsByRowsActuallyInserted(t *testing.T) {
	db := testDB(t)
	logs := NewLogRepo(db)
	usage := NewUsageRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)
	t.Cleanup(func() { db.Where("project_id = ?", projectID).Delete(&ProjectUsageDailyModel{}) })

	batch := []domain.Log{
		taggedLog(projectID, "one", "info", nil),
		taggedLog(projectID, "two", "info", nil),
		taggedLog(projectID, "three", "info", nil),
	}

	inserted, err := logs.CreateBatch(ctx, batch)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if inserted != 3 {
		t.Fatalf("want 3 inserted, got %d", inserted)
	}
	if err := usage.Add(ctx, projectID, time.Now(), inserted); err != nil {
		t.Fatalf("add: %v", err)
	}

	// The identical batch again, as a retry after a lost response.
	replayed, err := logs.CreateBatch(ctx, batch)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replayed != 0 {
		t.Errorf("a replayed batch stored %d rows, want 0", replayed)
	}
	if err := usage.Add(ctx, projectID, time.Now(), replayed); err != nil {
		t.Fatalf("add after replay: %v", err)
	}

	got, err := usage.CountForDay(ctx, projectID, time.Now())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if got != 3 {
		t.Errorf("the retry burned quota for rows it never stored: counter is %d, want 3", got)
	}

	// And genuinely new rows still move it.
	fresh, err := logs.CreateBatch(ctx, []domain.Log{taggedLog(projectID, "four", "info", nil)})
	if err != nil {
		t.Fatalf("fresh insert: %v", err)
	}
	if err := usage.Add(ctx, projectID, time.Now(), fresh); err != nil {
		t.Fatalf("add: %v", err)
	}
	if got, _ := usage.CountForDay(ctx, projectID, time.Now()); got != 4 {
		t.Errorf("want 4 after a genuinely new row, got %d", got)
	}
}

func TestPurgeUsageBeforeKeepsRecentDays(t *testing.T) {
	db := testDB(t)
	repo := NewUsageRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)
	t.Cleanup(func() { db.Where("project_id = ?", projectID).Delete(&ProjectUsageDailyModel{}) })

	now := time.Now()
	old := now.Add(-200 * 24 * time.Hour)
	if err := repo.Add(ctx, projectID, old, 1); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := repo.Add(ctx, projectID, now, 1); err != nil {
		t.Fatalf("add: %v", err)
	}

	purged, err := repo.PurgeUsageBefore(ctx, now.Add(-100*24*time.Hour))
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if purged != 1 {
		t.Errorf("want 1 purged, got %d", purged)
	}
	if got, _ := repo.CountForDay(ctx, projectID, now); got != 1 {
		t.Errorf("today's counter was purged too: %d", got)
	}
	if got, _ := repo.CountForDay(ctx, projectID, old); got != 0 {
		t.Errorf("the old counter survived: %d", got)
	}
}

// Adding nothing is a no-op, not a row full of zeroes.
func TestUsageAddIgnoresEmptyBatches(t *testing.T) {
	db := testDB(t)
	repo := NewUsageRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)
	t.Cleanup(func() { db.Where("project_id = ?", projectID).Delete(&ProjectUsageDailyModel{}) })

	if err := repo.Add(ctx, projectID, time.Now(), 0); err != nil {
		t.Fatalf("add: %v", err)
	}

	var rows int64
	db.Model(&ProjectUsageDailyModel{}).Where("project_id = ?", projectID).Count(&rows)
	if rows != 0 {
		t.Errorf("an empty batch created %d rows", rows)
	}
}
