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

// AutoMigrate runs at boot and a failure is fatal, so a column this table
// cannot take is an instance that will not start.
//
// This is not hypothetical. `update_check_enabled` shipped as `not null` with
// no default, which is fine on the empty table every test and every fresh
// install has, and impossible on one with a saved settings row: Postgres has
// nothing to put in the existing row and refuses the ALTER. The dev server
// stopped booting. The local database had no settings row, so nothing here
// noticed.
//
// The test seeds a row first and then migrates, which is the order a real
// upgrade happens in.
func TestInstanceSettingsMigratesOntoATableThatHasARow(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	repo := NewInstanceSettingsRepo(db)

	clearInstanceSettings(t, repo)
	settings := domain.DefaultInstanceSettings()
	settings.Timezone = "Asia/Kolkata"
	if err := repo.Save(ctx, &settings); err != nil {
		t.Fatalf("seed the row an upgrade would find: %v", err)
	}

	// Drop the column so the migration genuinely has to add it. Without this
	// the test database already has it, AutoMigrate does nothing, and the test
	// passes no matter what the tag says — which is exactly how the real
	// failure got through.
	if err := db.Exec(`ALTER TABLE instance_settings DROP COLUMN IF EXISTS update_check_enabled`).Error; err != nil {
		t.Fatalf("drop the column to simulate the older schema: %v", err)
	}
	t.Cleanup(func() { db.AutoMigrate(&InstanceSettingsModel{}) })

	// The same call main.go makes on the way up.
	if err := db.AutoMigrate(&InstanceSettingsModel{}); err != nil {
		t.Fatalf("migrating onto a populated table failed, so the server would not boot: %v", err)
	}

	got, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Timezone != "Asia/Kolkata" {
		t.Errorf("the existing row did not survive: timezone = %q", got.Timezone)
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
	//
	// UpdateCheckEnabled is false here on purpose, and it is the one value in
	// this struct that can be lost on the create path rather than the update
	// one: it is a bool whose column carries `default:true`, and GORM omits a
	// zero-valued field from the INSERT so the database default takes over. An
	// operator who turns the check off before anything else was ever saved
	// would find it back on.
	created := domain.InstanceSettings{
		Timezone:           "Asia/Kolkata",
		TimeFormat:         domain.TimeFormat12,
		RetentionDays:      90,
		PurgeAfterDays:     14,
		MaxUploadBytes:     25 << 20,
		DailyLogCap:        50_000,
		UpdateCheckEnabled: false,
	}
	if err := repo.Save(ctx, &created); err != nil {
		t.Fatalf("create: %v", err)
	}
	assertSettings(t, repo, created, "after the creating save")

	// Second save takes the update path, which is the one with the map.
	updated := domain.InstanceSettings{
		Timezone:           "Europe/London",
		TimeFormat:         domain.TimeFormat24,
		RetentionDays:      365,
		PurgeAfterDays:     30,
		MaxUploadBytes:     128 << 20,
		DailyLogCap:        0,
		UpdateCheckEnabled: true,
	}
	if err := repo.Save(ctx, &updated); err != nil {
		t.Fatalf("update: %v", err)
	}
	assertSettings(t, repo, updated, "after the updating save")

	// And back to false through the update path. This is the map-versus-struct
	// trap in its purest form: with Updates given a struct, false is a zero
	// value, so turning the check off would report success and change nothing.
	off := updated
	off.UpdateCheckEnabled = false
	if err := repo.Save(ctx, &off); err != nil {
		t.Fatalf("update back to off: %v", err)
	}
	assertSettings(t, repo, off, "after turning the update check off")
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
	if got.TimeFormat != want.TimeFormat {
		t.Errorf("%s: time_format = %q, want %q", when, got.TimeFormat, want.TimeFormat)
	}
	if got.DailyLogCap != want.DailyLogCap {
		t.Errorf("%s: daily_log_cap = %d, want %d", when, got.DailyLogCap, want.DailyLogCap)
	}
	if got.UpdateCheckEnabled != want.UpdateCheckEnabled {
		t.Errorf("%s: update_check_enabled = %v, want %v", when, got.UpdateCheckEnabled, want.UpdateCheckEnabled)
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
