package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/pkg/cslog"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UsageRepo struct {
	db *gorm.DB
}

func NewUsageRepo(db *gorm.DB) *UsageRepo {
	return &UsageRepo{db: db}
}

// CountForDay is what a project has already stored today. A missing row is
// zero, which is the normal state at the start of every day.
func (r *UsageRepo) CountForDay(ctx context.Context, projectID uuid.UUID, day time.Time) (int64, error) {
	db := getDB(ctx, r.db)

	var model ProjectUsageDailyModel
	err := db.WithContext(ctx).
		Where("project_id = ? AND day = ?", projectID, domain.UsageDay(day)).
		First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		cslog.L(ctx).WithError(err).Error("DB: read daily usage failed")
		return 0, err
	}
	return model.LogCount, nil
}

// Add increments a project's count for the day.
//
// Called inside the same transaction as the insert it is counting. Outside and
// after, a process killed between the two gives the project free quota;
// outside and before, a rolled-back insert charges for rows that were never
// written. Inside, the count and the rows it counts commit together.
func (r *UsageRepo) Add(ctx context.Context, projectID uuid.UUID, day time.Time, n int64) error {
	if n <= 0 {
		return nil
	}

	db := getDB(ctx, r.db)
	row := ProjectUsageDailyModel{
		ProjectID: projectID,
		Day:       domain.UsageDay(day),
		LogCount:  n,
	}

	err := db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "project_id"}, {Name: "day"}},
		DoUpdates: clause.Assignments(map[string]any{
			"log_count":  gorm.Expr("project_usage_daily.log_count + ?", n),
			"updated_at": time.Now(),
		}),
	}).Create(&row).Error
	if err != nil {
		cslog.L(ctx).WithError(err).Error("DB: increment daily usage failed")
		return err
	}
	return nil
}

// PurgeUsageBefore drops counters for days nobody will ask about again. The
// table is one small row per project per day, but nothing else would ever
// delete from it.
func (r *UsageRepo) PurgeUsageBefore(ctx context.Context, day time.Time) (int64, error) {
	db := getDB(ctx, r.db)
	result := db.WithContext(ctx).
		Where("day < ?", domain.UsageDay(day)).
		Delete(&ProjectUsageDailyModel{})
	if result.Error != nil {
		cslog.L(ctx).WithError(result.Error).Error("DB: purge daily usage failed")
		return 0, result.Error
	}
	return result.RowsAffected, nil
}
