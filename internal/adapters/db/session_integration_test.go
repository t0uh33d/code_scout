package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
)

func strp(s string) *string { return &s }

// The SDK re-sends the session record with every batch, so the repeat is the
// normal case. What it must never do is move started_at — otherwise every
// session looks like it began at its most recent sync, and "how long did this
// session run" becomes meaningless.
func TestSessionUpsertKeepsTheOriginalStart(t *testing.T) {
	db := testDB(t)
	repo := NewSessionRepo(db)
	ctx := context.Background()
	projectID := seedProject(t, db)

	sessionID := uuid.New()
	started := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	t.Cleanup(func() { db.Unscoped().Where("id = ?", sessionID).Delete(&SessionModel{}) })

	first := domain.Session{
		ID: sessionID, ProjectID: projectID,
		DeviceModel: strp("Pixel 7"), OSName: strp("Android"), OSVersion: strp("14"),
		StartedAt: started, LastSeenAt: started,
	}
	if err := repo.Upsert(ctx, &first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	// A later batch, claiming a much later start.
	later := time.Now().Truncate(time.Second)
	second := first
	second.StartedAt = later
	second.LastSeenAt = later
	second.UserID = strp("u_8812")
	if err := repo.Upsert(ctx, &second); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := repo.GetByID(ctx, projectID, sessionID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.StartedAt.Equal(started) {
		t.Errorf("started_at moved: want %s, got %s", started, got.StartedAt)
	}
	if !got.LastSeenAt.Equal(later) {
		t.Errorf("last_seen_at should advance: want %s, got %s", later, got.LastSeenAt)
	}
	// setUser() can be called mid-session, so identity has to be revisable.
	if got.UserID == nil || *got.UserID != "u_8812" {
		t.Errorf("a later batch should be able to set the user, got %v", got.UserID)
	}

	var rows int64
	db.Model(&SessionModel{}).Where("id = ?", sessionID).Count(&rows)
	if rows != 1 {
		t.Errorf("re-sending a session should update, not duplicate: %d rows", rows)
	}
}

// The project comes from the authenticated headers. Re-sending a session with
// a different project must not move it, or a client could write into a project
// it has credentials for and then read it from one it does not.
func TestSessionUpsertCannotMoveProject(t *testing.T) {
	db := testDB(t)
	repo := NewSessionRepo(db)
	ctx := context.Background()
	mine := seedProject(t, db)
	theirs := seedProject(t, db)

	sessionID := uuid.New()
	t.Cleanup(func() { db.Unscoped().Where("id = ?", sessionID).Delete(&SessionModel{}) })

	now := time.Now()
	original := domain.Session{ID: sessionID, ProjectID: mine, StartedAt: now, LastSeenAt: now}
	if err := repo.Upsert(ctx, &original); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	moved := original
	moved.ProjectID = theirs
	if err := repo.Upsert(ctx, &moved); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	if _, err := repo.GetByID(ctx, mine, sessionID); err != nil {
		t.Errorf("the session should still belong to its original project: %v", err)
	}
	if _, err := repo.GetByID(ctx, theirs, sessionID); err == nil {
		t.Error("the session must not have moved to another project")
	}
}

// A session read must be scoped, so an id guessed from another project is a
// miss rather than a disclosure.
func TestSessionGetIsProjectScoped(t *testing.T) {
	db := testDB(t)
	repo := NewSessionRepo(db)
	ctx := context.Background()
	mine := seedProject(t, db)
	theirs := seedProject(t, db)

	sessionID := uuid.New()
	t.Cleanup(func() { db.Unscoped().Where("id = ?", sessionID).Delete(&SessionModel{}) })

	now := time.Now()
	s := domain.Session{ID: sessionID, ProjectID: mine, StartedAt: now, LastSeenAt: now}
	if err := repo.Upsert(ctx, &s); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if _, err := repo.GetByID(ctx, theirs, sessionID); err == nil {
		t.Error("another project should not be able to read this session")
	}
}

// Display labels are what the sessions and devices lists render, and a missing
// user is a real answer rather than a blank.
func TestSessionLabels(t *testing.T) {
	full := domain.Session{
		DeviceModel: strp("Pixel 7"), OSName: strp("Android"), OSVersion: strp("14"),
		UserID: strp("u_8812"),
	}
	if got := full.DeviceLabel(); got != "Pixel 7 · Android 14" {
		t.Errorf("got %q", got)
	}
	if got := full.UserLabel(); got != "u_8812" {
		t.Errorf("got %q", got)
	}
	if !full.IsIdentified() {
		t.Error("a session with a user id is identified")
	}

	bare := domain.Session{}
	if got := bare.DeviceLabel(); got != "Unknown device" {
		t.Errorf("got %q", got)
	}
	if got := bare.UserLabel(); got != "anonymous" {
		t.Errorf("identity is opt-in, so an unset user reads as anonymous, got %q", got)
	}
	if bare.IsIdentified() {
		t.Error("a session with no user id is not identified")
	}
}
