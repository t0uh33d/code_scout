package db

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/t0uh33d/code_scout/internal/domain"
)

// Runs against a real Postgres because the behaviour under test is the unique
// index and ON CONFLICT, neither of which a mock would exercise. Set
// CS_TEST_DB to a DSN to enable, for example:
//
//	CS_TEST_DB="host=127.0.0.1 port=5432 user=code_scout password=code_scout dbname=code_scout_test sslmode=disable"
func testDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("CS_TEST_DB")
	if dsn == "" {
		t.Skip("CS_TEST_DB not set, skipping integration test")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// seedProject satisfies the foreign key on logs.project_id.
func seedProject(t *testing.T, db *gorm.DB) uuid.UUID {
	t.Helper()

	p := ProjectModel{Name: "test-" + uuid.NewString(), Description: "integration"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	t.Cleanup(func() {
		db.Unscoped().Where("project_id = ?", p.ID).Delete(&LogModel{})
		db.Unscoped().Delete(&ProjectModel{}, "id = ?", p.ID)
	})
	return p.ID
}

// TestPurgeOrphanedLogsIsBounded pins the two properties the delete path relies
// on: it never exceeds its batch size, and it never touches a live project.
func TestPurgeOrphanedLogsIsBounded(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()

	doomed := seedProject(t, db)
	kept := seedProject(t, db)

	ids := func(n int) []*uuid.UUID {
		out := make([]*uuid.UUID, n)
		for i := range out {
			id := uuid.New()
			out[i] = &id
		}
		return out
	}
	if err := repo.CreateBatch(ctx, batch(doomed, ids(3))); err != nil {
		t.Fatalf("seed doomed logs: %v", err)
	}
	if err := repo.CreateBatch(ctx, batch(kept, ids(3))); err != nil {
		t.Fatalf("seed kept logs: %v", err)
	}

	// Nothing is orphaned until the project is deleted.
	if n, err := repo.PurgeOrphanedLogs(ctx, 10); err != nil || n != 0 {
		t.Fatalf("purge before delete = %d (err %v), want 0", n, err)
	}

	if err := db.Where("id = ?", doomed).Delete(&ProjectModel{}).Error; err != nil {
		t.Fatalf("soft-delete project: %v", err)
	}

	// Respects the limit rather than deleting everything in one statement.
	n, err := repo.PurgeOrphanedLogs(ctx, 2)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if n != 2 {
		t.Fatalf("first batch = %d, want 2", n)
	}

	total := int64(2)
	for range 5 {
		n, err := repo.PurgeOrphanedLogs(ctx, 2)
		if err != nil {
			t.Fatalf("purge: %v", err)
		}
		total += n
		if n < 2 {
			break
		}
	}
	if total != 3 {
		t.Fatalf("purged %d rows in total, want 3", total)
	}

	var doomedLeft, keptLeft int64
	db.Unscoped().Model(&LogModel{}).Where("project_id = ?", doomed).Count(&doomedLeft)
	db.Unscoped().Model(&LogModel{}).Where("project_id = ?", kept).Count(&keptLeft)
	if doomedLeft != 0 {
		t.Errorf("deleted project still has %d logs", doomedLeft)
	}
	if keptLeft != 3 {
		t.Errorf("live project has %d logs, want 3 untouched", keptLeft)
	}
}

func batch(projectID uuid.UUID, clientIDs []*uuid.UUID) []domain.Log {
	logs := make([]domain.Log, 0, len(clientIDs))
	for i, cid := range clientIDs {
		logs = append(logs, domain.Log{
			ClientID:  cid,
			ProjectID: projectID,
			SessionID: uuid.New(),
			Level:     "info",
			Message:   "message",
			TimeStamp: time.Now().Add(-time.Duration(i) * time.Second),
		})
	}
	return logs
}

func countLogs(t *testing.T, db *gorm.DB, projectID uuid.UUID) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&LogModel{}).Where("project_id = ?", projectID).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// The case this exists for: the server commits a batch, the response is lost,
// and the SDK re-uploads the identical batch.
func TestCreateBatchIsIdempotent(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)

	ids := []*uuid.UUID{ptrUUID(uuid.New()), ptrUUID(uuid.New()), ptrUUID(uuid.New())}

	if err := repo.CreateBatch(ctx, batch(projectID, ids)); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if got := countLogs(t, db, projectID); got != 3 {
		t.Fatalf("after first insert = %d, want 3", got)
	}

	if err := repo.CreateBatch(ctx, batch(projectID, ids)); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := countLogs(t, db, projectID); got != 3 {
		t.Errorf("after retry = %d, want 3 (rows were duplicated)", got)
	}
}

// A partly-overlapping retry must insert only the rows not already stored,
// which is what happens when a batch is split or resent alongside new logs.
func TestCreateBatchInsertsOnlyNewRows(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)

	a, b, c := ptrUUID(uuid.New()), ptrUUID(uuid.New()), ptrUUID(uuid.New())

	if err := repo.CreateBatch(ctx, batch(projectID, []*uuid.UUID{a, b})); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := repo.CreateBatch(ctx, batch(projectID, []*uuid.UUID{b, c})); err != nil {
		t.Fatalf("overlapping insert: %v", err)
	}

	if got := countLogs(t, db, projectID); got != 3 {
		t.Errorf("count = %d, want 3 (a, b, c exactly once)", got)
	}
}

// Logs with no client id cannot be deduplicated, so they must all insert
// rather than collide with each other on a shared null.
func TestCreateBatchKeepsLogsWithoutClientID(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)

	if err := repo.CreateBatch(ctx, batch(projectID, []*uuid.UUID{nil, nil, nil})); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if got := countLogs(t, db, projectID); got != 3 {
		t.Errorf("count = %d, want 3 (nulls must not collide)", got)
	}
}

// The same client id under two projects is two different logs, so one
// project's clients can never suppress another's rows.
func TestCreateBatchScopesDeduplicationToProject(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	projectA := seedProject(t, db)
	projectB := seedProject(t, db)

	shared := ptrUUID(uuid.New())

	if err := repo.CreateBatch(ctx, batch(projectA, []*uuid.UUID{shared})); err != nil {
		t.Fatalf("project A: %v", err)
	}
	if err := repo.CreateBatch(ctx, batch(projectB, []*uuid.UUID{shared})); err != nil {
		t.Fatalf("project B: %v", err)
	}

	if got := countLogs(t, db, projectA); got != 1 {
		t.Errorf("project A = %d, want 1", got)
	}
	if got := countLogs(t, db, projectB); got != 1 {
		t.Errorf("project B = %d, want 1", got)
	}
}

func ptrUUID(u uuid.UUID) *uuid.UUID { return &u }

// taggedLog is one row with an exact tag set. A nil tags argument means the
// column is NULL, which is the case the exclusion filter gets wrong if the SQL
// is written the obvious way.
func taggedLog(projectID uuid.UUID, message string, level string, tags []string) domain.Log {
	clientID := uuid.New()
	l := domain.Log{
		ClientID:  &clientID,
		ProjectID: projectID,
		SessionID: uuid.New(),
		Level:     level,
		Message:   message,
		TimeStamp: time.Now(),
	}
	if tags != nil {
		raw, _ := json.Marshal(tags)
		msg := json.RawMessage(raw)
		l.Tags = &msg
	}
	return l
}

func listWith(t *testing.T, repo *LogRepo, projectID uuid.UUID, f domain.SearchFilter) []string {
	t.Helper()
	res, err := repo.List(context.Background(), domain.LogListOpts{
		ProjectID: projectID, Filter: f, Limit: 100,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	out := make([]string, 0, len(res.Items))
	for _, l := range res.Items {
		out = append(out, l.Message)
	}
	return out
}

// The bug this pins: `NOT (tags @> '["x"]')` is NULL for a log with no tags,
// and NULL is not true, so every untagged log silently disappears the moment
// you exclude anything. A developer excluding noise would lose exactly the
// plain logs they were trying to see.
func TestExcludeTagsKeepsUntaggedLogs(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)

	logs := []domain.Log{
		taggedLog(projectID, "has-noise", "info", []string{"noise"}),
		taggedLog(projectID, "has-other", "info", []string{"checkout"}),
		taggedLog(projectID, "no-tags-at-all", "info", nil),
		taggedLog(projectID, "empty-tag-list", "info", []string{}),
	}
	if err := repo.CreateBatch(ctx, logs); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got := listWith(t, repo, projectID, domain.SearchFilter{ExcludeTags: []string{"noise"}})
	want := map[string]bool{"has-other": true, "no-tags-at-all": true, "empty-tag-list": true}
	if len(got) != len(want) {
		t.Fatalf("excluding noise should leave %d logs, got %d: %v", len(want), len(got), got)
	}
	for _, m := range got {
		if !want[m] {
			t.Errorf("%q should not have survived the exclusion", m)
		}
	}
}

// Included tags AND together; excluded tags apply on top, so the pair the
// toolbar produces (`tag:checkout -tag:heartbeat`) is meaningful.
func TestIncludeAndExcludeTagsCombine(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)

	logs := []domain.Log{
		taggedLog(projectID, "checkout-only", "info", []string{"checkout"}),
		taggedLog(projectID, "checkout-and-heartbeat", "info", []string{"checkout", "heartbeat"}),
		taggedLog(projectID, "heartbeat-only", "info", []string{"heartbeat"}),
	}
	if err := repo.CreateBatch(ctx, logs); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got := listWith(t, repo, projectID, domain.SearchFilter{
		Tags: []string{"checkout"}, ExcludeTags: []string{"heartbeat"},
	})
	if len(got) != 1 || got[0] != "checkout-only" {
		t.Errorf("want only checkout-only, got %v", got)
	}
}

// Levels OR together, which is what the toolbar's toggles mean.
func TestLevelsAreAnOr(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)

	logs := []domain.Log{
		taggedLog(projectID, "an-error", "error", nil),
		taggedLog(projectID, "a-fatal", "fatal", nil),
		taggedLog(projectID, "an-info", "info", nil),
	}
	if err := repo.CreateBatch(ctx, logs); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got := listWith(t, repo, projectID, domain.SearchFilter{Levels: []string{"error", "fatal"}})
	if len(got) != 2 {
		t.Fatalf("want 2 logs, got %d: %v", len(got), got)
	}

	// No levels means no filter, not "no logs".
	if all := listWith(t, repo, projectID, domain.SearchFilter{}); len(all) != 3 {
		t.Errorf("an empty level list should not filter anything, got %d", len(all))
	}
}
