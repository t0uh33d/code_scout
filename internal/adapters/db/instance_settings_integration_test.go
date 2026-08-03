package db

import (
	"context"
	"testing"

	"github.com/getcodescout/code_scout/internal/domain"
)

// clearInstanceSettings keeps each test starting from the fresh-install state,
// since the table holds one shared row rather than per-test data.
func clearInstanceSettings(t *testing.T, repo *InstanceSettingsRepo) {
	t.Helper()
	repo.db.Unscoped().Where("1 = 1").Delete(&InstanceSettingsModel{})
	t.Cleanup(func() {
		repo.db.Unscoped().Where("1 = 1").Delete(&InstanceSettingsModel{})
	})
}

// A fresh instance has no row at all. That is the normal first-run state, so it
// must read as the defaults rather than as an error.
func TestInstanceSettingsDefaultsWhenNeverSaved(t *testing.T) {
	repo := NewInstanceSettingsRepo(testDB(t))
	clearInstanceSettings(t, repo)

	got, err := repo.Get(context.Background())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Timezone != domain.DefaultTimezone {
		t.Errorf("want the default %q, got %q", domain.DefaultTimezone, got.Timezone)
	}
}

// Save has to create the row the first time and update it afterwards, without
// ever leaving two rows behind — Get reads the first by id, so a second row
// would make saves appear to do nothing.
func TestInstanceSettingsSaveCreatesThenUpdatesOneRow(t *testing.T) {
	repo := NewInstanceSettingsRepo(testDB(t))
	ctx := context.Background()
	clearInstanceSettings(t, repo)

	if err := repo.Save(ctx, &domain.InstanceSettings{Timezone: "Asia/Kolkata"}); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := repo.Save(ctx, &domain.InstanceSettings{Timezone: "Europe/London"}); err != nil {
		t.Fatalf("second save: %v", err)
	}

	var rows int64
	if err := repo.db.Model(&InstanceSettingsModel{}).Count(&rows).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 1 {
		t.Errorf("want exactly 1 settings row, got %d", rows)
	}

	got, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Timezone != "Europe/London" {
		t.Errorf("want the latest save to win, got %q", got.Timezone)
	}
}

// The round trip is what the boot path depends on: whatever was saved has to
// come back loadable, or every timestamp silently renders in UTC.
func TestInstanceSettingsRoundTripsALoadableZone(t *testing.T) {
	repo := NewInstanceSettingsRepo(testDB(t))
	ctx := context.Background()
	clearInstanceSettings(t, repo)

	if err := repo.Save(ctx, &domain.InstanceSettings{Timezone: "Asia/Kolkata"}); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if loc := got.Location(); loc.String() != "Asia/Kolkata" {
		t.Errorf("stored zone did not survive the round trip, got %v", loc)
	}
}

// The highest-probability silent bug in this change: a domain field with a
// column but no key in Save's Updates map. The form reports "Saved", every view
// test passes, and nothing is ever written. Only reading the row back catches
// it, so every field goes through here.
func TestInstanceSettingsRoundTripsEveryField(t *testing.T) {
	repo := NewInstanceSettingsRepo(testDB(t))
	ctx := context.Background()
	clearInstanceSettings(t, repo)

	// First save creates the row.
	created := domain.InstanceSettings{
		Timezone:       "Asia/Kolkata",
		RetentionDays:  90,
		PurgeAfterDays: 14,
		MaxUploadBytes: 25 << 20,
	}
	if err := repo.Save(ctx, &created); err != nil {
		t.Fatalf("create: %v", err)
	}
	assertSettings(t, repo, created, "after the creating save")

	// Second save takes the update path, which is the one with the map.
	updated := domain.InstanceSettings{
		Timezone:       "Europe/London",
		RetentionDays:  365,
		PurgeAfterDays: 30,
		MaxUploadBytes: 128 << 20,
	}
	if err := repo.Save(ctx, &updated); err != nil {
		t.Fatalf("update: %v", err)
	}
	assertSettings(t, repo, updated, "after the updating save")
}

func assertSettings(t *testing.T, repo *InstanceSettingsRepo, want domain.InstanceSettings, when string) {
	t.Helper()
	got, err := repo.Get(context.Background())
	if err != nil {
		t.Fatalf("get %s: %v", when, err)
	}
	if got.Timezone != want.Timezone {
		t.Errorf("%s: timezone = %q, want %q", when, got.Timezone, want.Timezone)
	}
	if got.RetentionDays != want.RetentionDays {
		t.Errorf("%s: retention_days = %d, want %d", when, got.RetentionDays, want.RetentionDays)
	}
	if got.PurgeAfterDays != want.PurgeAfterDays {
		t.Errorf("%s: purge_after_days = %d, want %d", when, got.PurgeAfterDays, want.PurgeAfterDays)
	}
	if got.MaxUploadBytes != want.MaxUploadBytes {
		t.Errorf("%s: max_upload_bytes = %d, want %d", when, got.MaxUploadBytes, want.MaxUploadBytes)
	}
}

// A fresh instance must read as the full defaults, not as a zero struct — a
// zero RetentionDays would put the nightly cutoff at "now".
func TestInstanceSettingsFreshInstanceHasSafeDefaults(t *testing.T) {
	repo := NewInstanceSettingsRepo(testDB(t))
	clearInstanceSettings(t, repo)

	got, err := repo.Get(context.Background())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.RetentionDays != domain.DefaultRetentionDays {
		t.Errorf("retention = %d, want %d", got.RetentionDays, domain.DefaultRetentionDays)
	}
	if got.MaxUploadBytes != domain.DefaultMaxUploadBytes {
		t.Errorf("upload cap = %d, want %d", got.MaxUploadBytes, domain.DefaultMaxUploadBytes)
	}
	if !domain.ValidRetentionDays(got.RetentionDays) {
		t.Error("the fresh-install retention window must be safe to run against")
	}
}
