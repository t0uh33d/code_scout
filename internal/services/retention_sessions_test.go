package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/ports"
)

type fakeSessionRepo struct {
	ports.SessionRepository

	orphanPurges int
	purgeCalls   int
	purgedBefore time.Time
}

func (f *fakeSessionRepo) PurgeOrphanedSessions(ctx context.Context, limit int) (int64, error) {
	f.orphanPurges++
	return 0, nil
}

func (f *fakeSessionRepo) PurgeSessionsBefore(ctx context.Context, before time.Time) (int64, error) {
	f.purgeCalls++
	f.purgedBefore = before
	return 0, nil
}

// loadedSettings is an InstanceSettingsService that reports Loaded, since
// Cleanup refuses to delete anything against settings it never read.
func loadedSettings(t *testing.T) *InstanceSettingsService {
	t.Helper()
	s := NewInstanceSettingsService(&fakeSettingsRepo{stored: domain.InstanceSettings{
		Timezone:       domain.DefaultTimezone,
		RetentionDays:  domain.DefaultRetentionDays,
		PurgeAfterDays: domain.DefaultPurgeAfterDays,
		MaxUploadBytes: domain.DefaultMaxUploadBytes,
	}})
	if err := s.Load(context.Background()); err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if !s.Loaded() {
		t.Fatal("settings did not load, so Cleanup would skip every phase")
	}
	return s
}

// The sessions table is one row per app launch per device, written faster than
// anything else in the schema, and nothing deleted from it at all: not
// retention, not deleting the project. Whatever an operator set retention to,
// this table grew forever.
func TestRetentionReapsSessions(t *testing.T) {
	logs := &fakeLogRepo{}
	sessions := &fakeSessionRepo{}

	if err := NewRetentionService(logs, nil, sessions, loadedSettings(t)).
		Cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	if sessions.purgeCalls != 1 {
		t.Errorf("expired sessions purged %d times, want 1: the table grows without bound",
			sessions.purgeCalls)
	}
	if sessions.orphanPurges == 0 {
		t.Error("sessions belonging to deleted projects are never reaped: deleting a " +
			"project is O(1) by design, so the nightly job is where its rows actually go")
	}
}

// The cutoff has to be the horizon where logs are removed for good, not the one
// where they are soft-deleted. A session is what a log's session_id resolves
// through, so dropping it early would take those logs out of every
// session-scoped search without deleting them.
func TestSessionsOutliveTheSoftDeleteWindow(t *testing.T) {
	logs := &fakeLogRepo{}
	sessions := &fakeSessionRepo{}

	if err := NewRetentionService(logs, nil, sessions, loadedSettings(t)).
		Cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	if sessions.purgedBefore.After(logs.lastSoftDeleteBefore) {
		t.Errorf("sessions are purged at %v, later than logs are soft-deleted at %v: "+
			"logs would survive with no session to attribute them to",
			sessions.purgedBefore, logs.lastSoftDeleteBefore)
	}
}

// Retention must still refuse to run when the settings were never read, and
// that refusal has to cover the new phase as well as the old ones.
func TestUnloadedSettingsSpareTheSessionsToo(t *testing.T) {
	sessions := &fakeSessionRepo{}
	unloaded := NewInstanceSettingsService(&fakeSettingsRepo{
		getErr: errors.New("database is down"),
	})
	_ = unloaded.Load(context.Background())

	if err := NewRetentionService(&fakeLogRepo{}, nil, sessions, unloaded).
		Cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	if sessions.purgeCalls != 0 || sessions.orphanPurges != 0 {
		t.Error("sessions were deleted against settings that were never loaded")
	}
}
