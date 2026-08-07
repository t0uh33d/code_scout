package db

import (
	"context"
	"errors"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"gorm.io/gorm"
)

type InstanceSettingsRepo struct {
	db *gorm.DB
}

func NewInstanceSettingsRepo(db *gorm.DB) *InstanceSettingsRepo {
	return &InstanceSettingsRepo{db: db}
}

// Get returns the settings row, or the defaults when a fresh instance has never
// saved any. A missing row is the normal first-run state, not an error.
func (r *InstanceSettingsRepo) Get(ctx context.Context) (*domain.InstanceSettings, error) {
	db := getDB(ctx, r.db)

	var model InstanceSettingsModel
	err := db.WithContext(ctx).Order("id").First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		settings := domain.DefaultInstanceSettings()
		return &settings, nil
	}
	if err != nil {
		cslog.L(ctx).WithError(err).Error("DB: get instance settings failed")
		return nil, err
	}
	return &domain.InstanceSettings{
		Timezone:           model.Timezone,
		TimeFormat:         model.TimeFormat,
		RetentionDays:      model.RetentionDays,
		PurgeAfterDays:     model.PurgeAfterDays,
		MaxUploadBytes:     model.MaxUploadBytes,
		DailyLogCap:        model.DailyLogCap,
		// Nil only for a row written before the column existed, which the
		// column default has already handled by the time anything reads it.
		// Falling back to the domain default rather than to Go's false keeps
		// the answer the one we chose.
		UpdateCheckEnabled: model.UpdateCheckEnabled == nil || *model.UpdateCheckEnabled,
	}, nil
}

// Save writes the settings, creating the row on first use.
//
// Ordered by id and updating the first row rather than deleting and inserting:
// the row's id is stable, so anything that ever references it keeps working.
func (r *InstanceSettingsRepo) Save(ctx context.Context, settings *domain.InstanceSettings) error {
	db := getDB(ctx, r.db)

	var model InstanceSettingsModel
	err := db.WithContext(ctx).Order("id").First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return db.WithContext(ctx).Create(&InstanceSettingsModel{
			Timezone:           settings.Timezone,
			TimeFormat:         settings.TimeFormat,
			RetentionDays:      settings.RetentionDays,
			PurgeAfterDays:     settings.PurgeAfterDays,
			MaxUploadBytes:     settings.MaxUploadBytes,
			DailyLogCap:        settings.DailyLogCap,
			UpdateCheckEnabled: &settings.UpdateCheckEnabled,
		}).Error
	}
	if err != nil {
		return err
	}

	// The map form, not the struct: Updates with a struct skips zero values, so
	// clearing a setting back to empty would silently no-op.
	//
	// Every field goes in this map. A domain field with a column but no key
	// here saves nothing, reports success, and passes every test that does not
	// read the row back — which is why the integration test round-trips all of
	// them.
	return db.WithContext(ctx).Model(&model).
		Updates(map[string]any{
			"timezone":         settings.Timezone,
			"time_format":      settings.TimeFormat,
			"retention_days":   settings.RetentionDays,
			"purge_after_days": settings.PurgeAfterDays,
			"max_upload_bytes": settings.MaxUploadBytes,
			"daily_log_cap":    settings.DailyLogCap,
			// The map form earns its keep here: false is this field's zero
			// value, so with the struct form turning the check off would report
			// success and change nothing.
			"update_check_enabled": settings.UpdateCheckEnabled,
		}).Error
}
