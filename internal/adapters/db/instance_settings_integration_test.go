package db

import (
	"context"
	"testing"

	"github.com/t0uh33d/code_scout/internal/domain"
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
