package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/ports"
	"github.com/getcodescout/code_scout/pkg/utils"
)

// fakeSettingsRepo stands in for the database so these tests are about the
// service's own rules: what it refuses, what it caches, and what it never
// disturbs.
type fakeSettingsRepo struct {
	stored  domain.InstanceSettings
	saves   int
	getErr  error
	saveErr error
}

func (f *fakeSettingsRepo) Get(ctx context.Context) (*domain.InstanceSettings, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	s := f.stored
	return &s, nil
}

func (f *fakeSettingsRepo) Save(ctx context.Context, s *domain.InstanceSettings) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saves++
	f.stored = *s
	return nil
}

func newTestSettings(t *testing.T) (*InstanceSettingsService, *fakeSettingsRepo) {
	t.Helper()
	repo := &fakeSettingsRepo{stored: domain.DefaultInstanceSettings()}
	svc := NewInstanceSettingsService(repo)
	if err := svc.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}
	return svc, repo
}

func TestUpdateRetentionStoresAndCaches(t *testing.T) {
	svc, repo := newTestSettings(t)

	if _, err := svc.UpdateRetention(context.Background(), "90", "14"); err != nil {
		t.Fatalf("update: %v", err)
	}

	// Visible from Current() before the call returns, or the page saves and
	// still renders the old value.
	if got := svc.Current().RetentionDays; got != 90 {
		t.Errorf("cache says %d days, want 90", got)
	}
	if repo.stored.PurgeAfterDays != 14 {
		t.Errorf("stored %d, want 14", repo.stored.PurgeAfterDays)
	}
}

// Every setter starts from Current() and writes the whole struct back, so one
// card cannot blank out another card's field.
func TestUpdatesDoNotDisturbOtherSettings(t *testing.T) {
	svc, _ := newTestSettings(t)
	ctx := context.Background()

	if _, err := svc.UpdateTimezone(ctx, "Asia/Kolkata"); err != nil {
		t.Fatalf("timezone: %v", err)
	}
	if _, err := svc.UpdateRetention(ctx, "90", "14"); err != nil {
		t.Fatalf("retention: %v", err)
	}
	if _, err := svc.UpdateLimits(ctx, "25", "0"); err != nil {
		t.Fatalf("limits: %v", err)
	}

	got := svc.Current()
	if got.Timezone != "Asia/Kolkata" {
		t.Errorf("the timezone was lost: %q", got.Timezone)
	}
	if got.RetentionDays != 90 {
		t.Errorf("retention was lost: %d", got.RetentionDays)
	}
	if got.MaxUploadBytes != 25<<20 {
		t.Errorf("the upload cap was lost: %d", got.MaxUploadBytes)
	}
}

// A refused value must leave the stored settings untouched, not partly applied.
func TestRefusedValuesChangeNothing(t *testing.T) {
	ctx := context.Background()

	for _, c := range []struct {
		name       string
		call       func(*InstanceSettingsService) (int, error)
		wantDetail string
	}{
		{"retention of zero", func(s *InstanceSettingsService) (int, error) {
			return s.UpdateRetention(ctx, "0", "7")
		}, "retention_days"},
		{"retention that is not a number", func(s *InstanceSettingsService) (int, error) {
			return s.UpdateRetention(ctx, "abc", "7")
		}, "retention_days"},
		{"purge of zero", func(s *InstanceSettingsService) (int, error) {
			return s.UpdateRetention(ctx, "30", "0")
		}, "purge_after_days"},
		{"an empty upload size", func(s *InstanceSettingsService) (int, error) {
			return s.UpdateLimits(ctx, "", "0")
		}, "max_upload_mb"},
		{"an upload size past the ceiling", func(s *InstanceSettingsService) (int, error) {
			return s.UpdateLimits(ctx, "99999", "0")
		}, "max_upload_mb"},
	} {
		t.Run(c.name, func(t *testing.T) {
			svc, repo := newTestSettings(t)
			before := svc.Current()

			_, err := c.call(svc)
			if err == nil {
				t.Fatal("want a refusal")
			}
			if repo.saves != 0 {
				t.Errorf("a refused value still wrote to the database")
			}
			if svc.Current() != before {
				t.Errorf("a refused value changed the cache: %+v", svc.Current())
			}
			if !hasFieldError(err, c.wantDetail) {
				t.Errorf("want an inline error on %q, got %v", c.wantDetail, err)
			}
		})
	}
}

// Both fields are checked before either is applied, so a form with two bad
// values shows two errors rather than only the first.
func TestRetentionReportsBothFieldsAtOnce(t *testing.T) {
	svc, _ := newTestSettings(t)

	_, err := svc.UpdateRetention(context.Background(), "0", "0")
	if err == nil {
		t.Fatal("want a refusal")
	}
	if !hasFieldError(err, "retention_days") || !hasFieldError(err, "purge_after_days") {
		t.Errorf("want an error against both fields, got %v", err)
	}
}

// Loaded() is what stops a destructive job running against defaults it never
// actually read.
func TestLoadedReportsWhetherSettingsWereRead(t *testing.T) {
	repo := &fakeSettingsRepo{getErr: errors.New("database is down")}
	svc := NewInstanceSettingsService(repo)

	if svc.Loaded() {
		t.Error("nothing has been read yet")
	}
	if err := svc.Load(context.Background()); err == nil {
		t.Fatal("want the load to fail")
	}
	if svc.Loaded() {
		t.Error("a failed load must not report as loaded")
	}
	// It still serves usable defaults, so the dashboard renders.
	if svc.Current().Timezone != domain.DefaultTimezone {
		t.Error("a failed load should leave usable defaults")
	}
}

// Retention deletes rows. Running it against defaults the service never read
// would throw away logs an operator had asked to keep.
func TestRetentionSkipsWhenSettingsWereNeverLoaded(t *testing.T) {
	repo := &fakeSettingsRepo{getErr: errors.New("database is down")}
	settings := NewInstanceSettingsService(repo)
	_ = settings.Load(context.Background())

	logs := &fakeLogRepo{}
	if err := NewRetentionService(logs, nil, nil, settings).Cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if logs.softDeletes+logs.purges+logs.orphanPurges != 0 {
		t.Errorf("it deleted something with settings it never read: %+v", logs)
	}
}

// Defence in depth behind the form: a stored value that is somehow out of
// range is refused rather than obeyed.
func TestRetentionRefusesOutOfRangeSettings(t *testing.T) {
	repo := &fakeSettingsRepo{stored: domain.InstanceSettings{
		Timezone: domain.DefaultTimezone, RetentionDays: 0, PurgeAfterDays: 7,
		MaxUploadBytes: domain.DefaultMaxUploadBytes,
	}}
	settings := NewInstanceSettingsService(repo)
	if err := settings.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}

	logs := &fakeLogRepo{}
	if err := NewRetentionService(logs, nil, nil, settings).Cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if logs.softDeletes != 0 {
		t.Error("0 days would have soft-deleted every log in the instance")
	}
}

// The cron holds one service for the life of the process. A window changed at
// 14:00 has to apply at the 03:00 run without a restart.
func TestRetentionReadsTheWindowOnEveryRun(t *testing.T) {
	settings, _ := newTestSettings(t)
	logs := &fakeLogRepo{}
	retention := NewRetentionService(logs, nil, nil, settings)
	ctx := context.Background()

	if err := retention.Cleanup(ctx); err != nil {
		t.Fatalf("first run: %v", err)
	}
	firstCutoff := logs.lastSoftDeleteBefore

	if _, err := settings.UpdateRetention(ctx, "365", "30"); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := retention.Cleanup(ctx); err != nil {
		t.Fatalf("second run: %v", err)
	}

	// 365 days back is much earlier than 30 days back.
	if !logs.lastSoftDeleteBefore.Before(firstCutoff.Add(-300 * 24 * time.Hour)) {
		t.Errorf("the second run used a stale window: %s then %s",
			firstCutoff, logs.lastSoftDeleteBefore)
	}
}

// fakeLogRepo records what retention asked it to delete. The interface is
// embedded rather than fully implemented: retention touches three of its
// methods, and a nil panic on any other is a louder failure than a silent
// stub returning zero.
type fakeLogRepo struct {
	ports.LogRepository

	orphanPurges         int
	softDeletes          int
	purges               int
	lastSoftDeleteBefore time.Time
}

func (f *fakeLogRepo) PurgeOrphanedLogs(ctx context.Context, limit int) (int64, error) {
	f.orphanPurges++
	return 0, nil
}

func (f *fakeLogRepo) SoftDeleteBefore(ctx context.Context, before time.Time) (int64, error) {
	f.softDeletes++
	f.lastSoftDeleteBefore = before
	return 0, nil
}

func (f *fakeLogRepo) PurgeSoftDeleted(ctx context.Context, before time.Time) (int64, error) {
	f.purges++
	return 0, nil
}

func hasFieldError(err error, field string) bool {
	var appErr *utils.ErrorJson
	if !errors.As(err, &appErr) {
		return false
	}
	for _, f := range appErr.Errors {
		if f.Field == field {
			return true
		}
	}
	return false
}
