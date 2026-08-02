package db

import (
	"context"
	"errors"

	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/pkg/cslog"
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
		return &domain.InstanceSettings{Timezone: domain.DefaultTimezone}, nil
	}
	if err != nil {
		cslog.L(ctx).WithError(err).Error("DB: get instance settings failed")
		return nil, err
	}
	return &domain.InstanceSettings{Timezone: model.Timezone}, nil
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
		return db.WithContext(ctx).Create(&InstanceSettingsModel{Timezone: settings.Timezone}).Error
	}
	if err != nil {
		return err
	}

	// The map form, not the struct: Updates with a struct skips zero values, so
	// clearing a setting back to empty would silently no-op.
	return db.WithContext(ctx).Model(&model).
		Updates(map[string]any{"timezone": settings.Timezone}).Error
}
