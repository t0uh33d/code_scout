package db

import (
	"context"
	"encoding/json"
	"os"
	"strings"
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
	if _, err := repo.CreateBatch(ctx, batch(doomed, ids(3))); err != nil {
		t.Fatalf("seed doomed logs: %v", err)
	}
	if _, err := repo.CreateBatch(ctx, batch(kept, ids(3))); err != nil {
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

	if _, err := repo.CreateBatch(ctx, batch(projectID, ids)); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if got := countLogs(t, db, projectID); got != 3 {
		t.Fatalf("after first insert = %d, want 3", got)
	}

	if _, err := repo.CreateBatch(ctx, batch(projectID, ids)); err != nil {
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

	if _, err := repo.CreateBatch(ctx, batch(projectID, []*uuid.UUID{a, b})); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := repo.CreateBatch(ctx, batch(projectID, []*uuid.UUID{b, c})); err != nil {
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

	if _, err := repo.CreateBatch(ctx, batch(projectID, []*uuid.UUID{nil, nil, nil})); err != nil {
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

	if _, err := repo.CreateBatch(ctx, batch(projectA, []*uuid.UUID{shared})); err != nil {
		t.Fatalf("project A: %v", err)
	}
	if _, err := repo.CreateBatch(ctx, batch(projectB, []*uuid.UUID{shared})); err != nil {
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
	if _, err := repo.CreateBatch(ctx, logs); err != nil {
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
	if _, err := repo.CreateBatch(ctx, logs); err != nil {
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
	if _, err := repo.CreateBatch(ctx, logs); err != nil {
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

// errLog is one error occurrence with its fingerprint already computed, the
// way ingest stores it.
func errLog(projectID, sessionID uuid.UUID, message string, at time.Time) domain.Log {
	clientID := uuid.New()
	fp := domain.Fingerprint("error", message, false, nil, nil)
	l := domain.Log{
		ClientID:  &clientID,
		ProjectID: projectID,
		SessionID: sessionID,
		Level:     "error",
		Message:   message,
		TimeStamp: at,
	}
	if fp != "" {
		l.Fingerprint = &fp
	}
	return l
}

// The whole point of the Errors screen: one bug wearing many ids is one row,
// and the row says how many sessions it touched — 47 in one session is a bad
// afternoon, 47 across 40 is an outage.
func TestErrorGroupsCollapseOneBugWithManyIds(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)

	base := time.Now().Add(-time.Hour)
	sessionA, sessionB := uuid.New(), uuid.New()
	logs := []domain.Log{
		errLog(projectID, sessionA, "User 4821 not found", base),
		errLog(projectID, sessionA, "User 9134 not found", base.Add(time.Minute)),
		errLog(projectID, sessionB, "User 7 not found", base.Add(2*time.Minute)),
		errLog(projectID, sessionA, "Payment declined", base.Add(3*time.Minute)),
	}
	if _, err := repo.CreateBatch(ctx, logs); err != nil {
		t.Fatalf("seed: %v", err)
	}

	groups, err := repo.GetErrorGroups(ctx, projectID, nil, 50)
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("want 2 distinct problems, got %d: %+v", len(groups), groups)
	}

	// Most frequent first.
	top := groups[0]
	if top.Fingerprint != "User {n} not found" {
		t.Errorf("want the normalised key, got %q", top.Fingerprint)
	}
	if top.Count != 3 {
		t.Errorf("want 3 occurrences, got %d", top.Count)
	}
	if top.Sessions != 2 {
		t.Errorf("want 2 sessions, got %d", top.Sessions)
	}
	// The row shows a real message, not the key.
	if top.SampleMessage != "User 7 not found" {
		t.Errorf("want the latest raw message, got %q", top.SampleMessage)
	}
	if !top.FirstSeen.Before(top.LastSeen) {
		t.Errorf("first seen %s should precede last seen %s", top.FirstSeen, top.LastSeen)
	}
	if top.LatestLogID == uuid.Nil {
		t.Error("the group should link to its latest occurrence")
	}
}

// Every failed request carries the same message, so without endpoint
// fingerprinting they would all be one useless row.
func TestErrorGroupsSeparateNetworkEndpoints(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)

	netErr := func(method, url string, at time.Time) domain.Log {
		clientID := uuid.New()
		m, u := method, url
		fp := domain.Fingerprint("error", "Network Error", true, &m, &u)
		return domain.Log{
			ClientID: &clientID, ProjectID: projectID, SessionID: uuid.New(),
			Level: "error", Message: "Network Error", TimeStamp: at,
			IsNetworkCall: true, Method: &m, URL: &u, Fingerprint: &fp,
		}
	}

	base := time.Now().Add(-time.Hour)
	logs := []domain.Log{
		netErr("POST", "https://api.dev/v2/pay", base),
		netErr("POST", "https://api.dev/v2/pay", base.Add(time.Minute)),
		netErr("GET", "https://api.dev/v2/cart", base.Add(2*time.Minute)),
	}
	if _, err := repo.CreateBatch(ctx, logs); err != nil {
		t.Fatalf("seed: %v", err)
	}

	groups, err := repo.GetErrorGroups(ctx, projectID, nil, 50)
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("two endpoints failing should be two rows, got %d: %+v", len(groups), groups)
	}
	if groups[0].Fingerprint != "POST /v2/pay" || groups[0].Count != 2 {
		t.Errorf("want POST /v2/pay ×2 first, got %q ×%d", groups[0].Fingerprint, groups[0].Count)
	}
}

// Only errors are problems. An info log on the Errors screen would be noise.
func TestErrorGroupsIgnoreNonErrors(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)

	clientID := uuid.New()
	logs := []domain.Log{
		{ClientID: &clientID, ProjectID: projectID, SessionID: uuid.New(),
			Level: "info", Message: "User signed in", TimeStamp: time.Now()},
		errLog(projectID, uuid.New(), "Real problem", time.Now()),
	}
	if _, err := repo.CreateBatch(ctx, logs); err != nil {
		t.Fatalf("seed: %v", err)
	}

	groups, err := repo.GetErrorGroups(ctx, projectID, nil, 50)
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	if len(groups) != 1 || groups[0].Fingerprint != "Real problem" {
		t.Errorf("want only the error, got %+v", groups)
	}
}

// A group has no stack trace of its own, so the row shows the latest
// occurrence's. Picking any other one would show a trace that does not match
// the message printed above it.
func TestErrorGroupsDescribeTheirLatestOccurrence(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)

	withDetail := func(l domain.Log, frame string, tags []string) domain.Log {
		trace := json.RawMessage(`[{"index":0,"method":"` + frame + `","path":"lib/cart.dart","line":12,"column":3}]`)
		l.StackTrace = &trace
		raw, _ := json.Marshal(tags)
		msg := json.RawMessage(raw)
		l.Tags = &msg
		return l
	}

	base := time.Now().Add(-time.Hour)
	oldSession, newSession := uuid.New(), uuid.New()
	logs := []domain.Log{
		withDetail(errLog(projectID, oldSession, "User 1 not found", base), "oldFrame", []string{"stale"}),
		withDetail(errLog(projectID, newSession, "User 2 not found", base.Add(time.Minute)), "newFrame", []string{"checkout"}),
	}
	if _, err := repo.CreateBatch(ctx, logs); err != nil {
		t.Fatalf("seed: %v", err)
	}

	groups, err := repo.GetErrorGroups(ctx, projectID, nil, 50)
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("want 1 group, got %d", len(groups))
	}
	g := groups[0]
	if g.LatestSessionID != newSession {
		t.Errorf("want the newest session %s, got %s", newSession, g.LatestSessionID)
	}
	if g.StackTrace == nil || !strings.Contains(string(*g.StackTrace), "newFrame") {
		t.Errorf("want the newest stack trace, got %v", g.StackTrace)
	}
	if g.Tags == nil || !strings.Contains(string(*g.Tags), "checkout") {
		t.Errorf("want the newest tags, got %v", g.Tags)
	}
}

// What "View in logs" does. A text search for the sample message would find one
// occurrence of three, because the id in the message differs every time — which
// is the whole reason fingerprints exist.
func TestFingerprintFilterFindsEveryOccurrence(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)

	base := time.Now().Add(-time.Hour)
	logs := []domain.Log{
		errLog(projectID, uuid.New(), "User 4821 not found", base),
		errLog(projectID, uuid.New(), "User 9134 not found", base.Add(time.Minute)),
		errLog(projectID, uuid.New(), "User 7 not found", base.Add(2*time.Minute)),
		errLog(projectID, uuid.New(), "Payment declined", base.Add(3*time.Minute)),
	}
	if _, err := repo.CreateBatch(ctx, logs); err != nil {
		t.Fatalf("seed: %v", err)
	}

	groups, err := repo.GetErrorGroups(ctx, projectID, nil, 50)
	if err != nil {
		t.Fatalf("groups: %v", err)
	}

	// Straight from the group, the way the link on the screen is built.
	got := listWith(t, repo, projectID, domain.SearchFilter{Fingerprint: &groups[0].Fingerprint})
	if len(got) != 3 {
		t.Fatalf("the group says 3 occurrences, the filter found %d: %v", len(got), got)
	}
	for _, m := range got {
		if !strings.HasPrefix(m, "User ") {
			t.Errorf("%q is not part of this group", m)
		}
	}
}

// Groups are per project, so one team's errors never appear on another's screen.
func TestErrorGroupsAreProjectScoped(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	mine := seedProject(t, db)
	theirs := seedProject(t, db)

	if _, err := repo.CreateBatch(ctx, []domain.Log{
		errLog(mine, uuid.New(), "Mine broke", time.Now()),
		errLog(theirs, uuid.New(), "Theirs broke", time.Now()),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	groups, err := repo.GetErrorGroups(ctx, mine, nil, 50)
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	if len(groups) != 1 || groups[0].Fingerprint != "Mine broke" {
		t.Errorf("another project's errors leaked in: %+v", groups)
	}
}

// netPhase writes one phase of a call, the way ingest stores it: method and url
// promoted to columns, everything else left in the metadata.
func netPhase(projectID, sessionID, requestID uuid.UUID, phase string, method, rawURL string, status *int, at time.Time) domain.Log {
	clientID := uuid.New()
	p := domain.CallPhase(phase)
	m, u := method, rawURL
	message := "Network " + phase

	meta := json.RawMessage(`{"method":"` + method + `","url":"` + rawURL + `","headers":{"content-type":"application/json"}}`)

	l := domain.Log{
		ClientID:      &clientID,
		ProjectID:     projectID,
		SessionID:     sessionID,
		Level:         "debug",
		Message:       message,
		TimeStamp:     at,
		IsNetworkCall: true,
		RequestID:     &requestID,
		CallPhase:     &p,
		Method:        &m,
		URL:           &u,
		StatusCode:    status,
		Metadata:      &meta,
	}
	if phase == "error" {
		l.Level = "error"
		l.Message = "Network Error"
		fp := domain.Fingerprint("error", l.Message, true, &m, &u)
		l.Fingerprint = &fp
	}
	return l
}

func intp(n int) *int { return &n }

func callByPath(t *testing.T, calls []domain.NetworkCall, path string) domain.NetworkCall {
	t.Helper()
	for _, c := range calls {
		if c.Path() == path {
			return c
		}
	}
	t.Fatalf("no call for %s in %+v", path, calls)
	return domain.NetworkCall{}
}

// The point of the screen: three log rows become one call, and the four states
// are told apart. Without this the log viewer already shows the phases — badly.
func TestNetworkCallsPairPhasesAndStates(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)

	base := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	session := uuid.New()
	complete, failed, pending, orphan := uuid.New(), uuid.New(), uuid.New(), uuid.New()

	logs := []domain.Log{
		// Complete: request then response, 250ms apart.
		netPhase(projectID, session, complete, "request", "GET", "https://api.test/v2/cart", nil, base),
		netPhase(projectID, session, complete, "response", "GET", "https://api.test/v2/cart", intp(200), base.Add(250*time.Millisecond)),
		// Failed: request then error.
		netPhase(projectID, session, failed, "request", "POST", "https://api.test/v2/pay", nil, base.Add(time.Second)),
		netPhase(projectID, session, failed, "error", "POST", "https://api.test/v2/pay", nil, base.Add(2*time.Second)),
		// Pending: a request the app never saw the end of.
		netPhase(projectID, session, pending, "request", "GET", "https://api.test/v2/products", nil, base.Add(3*time.Second)),
		// Response with no request, which happens when a batch is split.
		netPhase(projectID, session, orphan, "response", "GET", "https://api.test/v2/orphan", intp(204), base.Add(4*time.Second)),
	}
	if _, err := repo.CreateBatch(ctx, logs); err != nil {
		t.Fatalf("seed: %v", err)
	}

	calls, err := repo.ListNetworkCalls(ctx, projectID, domain.NetworkFilter{}, 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(calls) != 4 {
		t.Fatalf("six phase logs should be four calls, got %d: %+v", len(calls), calls)
	}

	// Newest first.
	if calls[0].Path() != "/v2/orphan" {
		t.Errorf("want the newest call first, got %s", calls[0].Path())
	}

	got := callByPath(t, calls, "/v2/cart")
	if got.State() != domain.CallComplete {
		t.Errorf("want complete, got %s", got.State())
	}
	if got.StatusCode == nil || *got.StatusCode != 200 {
		t.Errorf("want 200, got %v", got.StatusCode)
	}
	if got.Duration() != 250*time.Millisecond {
		t.Errorf("duration should be request to response, got %s", got.Duration())
	}
	if got.Failed() {
		t.Error("a 200 is not a failure")
	}

	got = callByPath(t, calls, "/v2/pay")
	if got.State() != domain.CallFailed || !got.Failed() {
		t.Errorf("want a failed call, got %s", got.State())
	}
	if got.Method == nil || *got.Method != "POST" {
		t.Errorf("the method should survive the grouping, got %v", got.Method)
	}

	got = callByPath(t, calls, "/v2/products")
	if got.State() != domain.CallPending {
		t.Errorf("want pending, got %s", got.State())
	}
	// A pending call has no end, so timing it would be inventing a number.
	if got.Duration() != 0 {
		t.Errorf("a pending call has no duration, got %s", got.Duration())
	}

	got = callByPath(t, calls, "/v2/orphan")
	if got.State() != domain.CallOrphaned {
		t.Errorf("a response with no request is response-only, got %s", got.State())
	}
}

// A 4xx is a failure worth seeing even though the call completed. This is the
// difference between "did it answer" and "did it work".
func TestNetworkCallsTreatBadStatusAsFailed(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)

	base := time.Now().Add(-time.Hour)
	session := uuid.New()
	unauthorised := uuid.New()
	if _, err := repo.CreateBatch(ctx, []domain.Log{
		netPhase(projectID, session, unauthorised, "request", "GET", "https://api.test/v2/profile", nil, base),
		netPhase(projectID, session, unauthorised, "response", "GET", "https://api.test/v2/profile", intp(401), base.Add(time.Millisecond)),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	calls, err := repo.ListNetworkCalls(ctx, projectID, domain.NetworkFilter{}, 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(calls) != 1 || !calls[0].Failed() {
		t.Errorf("a 401 should read as failed, got %+v", calls)
	}
	if calls[0].State() != domain.CallComplete {
		t.Errorf("it still completed, got %s", calls[0].State())
	}
}

func TestNetworkFilters(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)

	base := time.Now().Add(-time.Hour)
	sessionA, sessionB := uuid.New(), uuid.New()
	cart, pay, profile, boom := uuid.New(), uuid.New(), uuid.New(), uuid.New()

	if _, err := repo.CreateBatch(ctx, []domain.Log{
		netPhase(projectID, sessionA, cart, "request", "GET", "https://api.test/v2/cart", nil, base),
		netPhase(projectID, sessionA, cart, "response", "GET", "https://api.test/v2/cart", intp(200), base.Add(time.Millisecond)),
		netPhase(projectID, sessionA, pay, "request", "POST", "https://api.test/v2/checkout/pay", nil, base.Add(time.Second)),
		netPhase(projectID, sessionA, pay, "response", "POST", "https://api.test/v2/checkout/pay", intp(201), base.Add(time.Second+time.Millisecond)),
		netPhase(projectID, sessionB, profile, "request", "GET", "https://api.test/v2/profile", nil, base.Add(2*time.Second)),
		netPhase(projectID, sessionB, profile, "response", "GET", "https://api.test/v2/profile", intp(404), base.Add(2*time.Second+time.Millisecond)),
		netPhase(projectID, sessionB, boom, "request", "DELETE", "https://api.test/v2/cart/items", nil, base.Add(3*time.Second)),
		netPhase(projectID, sessionB, boom, "error", "DELETE", "https://api.test/v2/cart/items", nil, base.Add(4*time.Second)),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	list := func(f domain.NetworkFilter) []string {
		t.Helper()
		calls, err := repo.ListNetworkCalls(ctx, projectID, f, 50)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		out := make([]string, 0, len(calls))
		for _, c := range calls {
			out = append(out, c.Path())
		}
		return out
	}

	// Path matches anywhere in the URL, and matches case-insensitively —
	// nobody types a path with the case it was logged in.
	if got := list(domain.NetworkFilter{Path: "CART"}); len(got) != 2 {
		t.Errorf("want both cart calls, got %v", got)
	}
	if got := list(domain.NetworkFilter{Method: "GET"}); len(got) != 2 {
		t.Errorf("want the two GETs, got %v", got)
	}
	if got := list(domain.NetworkFilter{Status: "2xx"}); len(got) != 2 {
		t.Errorf("want the 200 and the 201, got %v", got)
	}
	if got := list(domain.NetworkFilter{Status: "4xx"}); len(got) != 1 || got[0] != "/v2/profile" {
		t.Errorf("want only the 404, got %v", got)
	}
	// "failed" is both kinds: a bad status, and a call that never answered.
	if got := list(domain.NetworkFilter{Status: "failed"}); len(got) != 2 {
		t.Errorf("want the 404 and the transport error, got %v", got)
	}
	if got := list(domain.NetworkFilter{SessionID: &sessionA}); len(got) != 2 {
		t.Errorf("want only session A's calls, got %v", got)
	}
	// Filters combine rather than replacing each other.
	if got := list(domain.NetworkFilter{Method: "GET", Status: "4xx"}); len(got) != 1 || got[0] != "/v2/profile" {
		t.Errorf("want the GET that 404'd, got %v", got)
	}
}

// Plain logs are not calls, and another project's calls never appear.
func TestNetworkCallsIgnoreNonNetworkAndOtherProjects(t *testing.T) {
	db := testDB(t)
	repo := NewLogRepo(db)
	ctx := context.Background()
	mine := seedProject(t, db)
	theirs := seedProject(t, db)

	base := time.Now().Add(-time.Hour)
	if _, err := repo.CreateBatch(ctx, []domain.Log{
		taggedLog(mine, "just a log", "info", nil),
		netPhase(mine, uuid.New(), uuid.New(), "request", "GET", "https://api.test/v2/mine", nil, base),
		netPhase(theirs, uuid.New(), uuid.New(), "request", "GET", "https://api.test/v2/theirs", nil, base),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	calls, err := repo.ListNetworkCalls(ctx, mine, domain.NetworkFilter{}, 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(calls) != 1 || calls[0].Path() != "/v2/mine" {
		t.Errorf("want only this project's one call, got %+v", calls)
	}
}
