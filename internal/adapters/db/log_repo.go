package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/pkg/cslog"
	"gorm.io/gorm"
)

type LogRepo struct {
	db *gorm.DB
}

func NewLogRepo(db *gorm.DB) *LogRepo {
	return &LogRepo{db: db}
}

func (r *LogRepo) CreateBatch(ctx context.Context, logs []domain.Log) error {
	log := cslog.L(ctx)
	log.WithField("count", len(logs)).Debug("DB: CreateBatch logs")

	if len(logs) == 0 {
		return nil
	}

	db := getDB(ctx, r.db)
	models := make([]LogModel, 0, len(logs))
	for _, l := range logs {
		models = append(models, *LogDomainToModel(&l))
	}
	if err := db.CreateInBatches(models, 100).Error; err != nil {
		log.WithError(err).Error("DB: CreateBatch failed")
		return err
	}
	// Write generated IDs back so callers (e.g. the SSE publish path) don't
	// hand out zero-value UUIDs.
	for i := range models {
		logs[i].ID = models[i].ID
	}
	return nil
}

// List queries logs with filtering, cursor pagination, and ordering.
func (r *LogRepo) List(ctx context.Context, opts domain.LogListOpts) (*domain.LogListResult, error) {
	log := cslog.L(ctx)
	log.Debug("DB: ListLogs")

	db := getDB(ctx, r.db)
	query := db.WithContext(ctx).Model(&LogModel{}).Where("project_id = ?", opts.ProjectID)

	// Apply search filters
	f := opts.Filter
	if f.Level != nil {
		query = query.Where("level = ?", *f.Level)
	}
	if f.IsNetwork != nil {
		query = query.Where("is_network_call = ?", *f.IsNetwork)
	}
	if f.SessionID != nil {
		query = query.Where("session_id = ?", *f.SessionID)
	}
	if f.RequestID != nil {
		query = query.Where("request_id = ?", *f.RequestID)
	}
	if f.TextQuery != "" {
		query = query.Where("message LIKE ?", "%"+f.TextQuery+"%")
	}
	if len(f.Tags) > 0 {
		for _, tag := range f.Tags {
			// Tags stored as JSON array — use JSON_CONTAINS for MySQL
			tagJSON, _ := json.Marshal(tag)
			query = query.Where("JSON_CONTAINS(tags, ?)", string(tagJSON))
		}
	}

	// Keyset pagination: rows strictly before the cursor position.
	// id (UUIDv7, time-ordered) breaks ties within the same timestamp.
	if opts.Cursor != nil {
		query = query.Where("(time_stamp < ?) OR (time_stamp = ? AND id < ?)",
			opts.Cursor.Time, opts.Cursor.Time, opts.Cursor.ID)
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}

	// Fetch one extra to determine if there are more results
	var models []LogModel
	if err := query.Order("time_stamp DESC, id DESC").Limit(limit + 1).Find(&models).Error; err != nil {
		log.WithError(err).Error("DB: ListLogs query failed")
		return nil, err
	}

	hasMore := len(models) > limit
	if hasMore {
		models = models[:limit]
	}

	items := make([]domain.Log, 0, len(models))
	for _, m := range models {
		items = append(items, *LogModelToDomain(&m))
	}

	var nextCursor *domain.LogCursor
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		nextCursor = &domain.LogCursor{Time: last.TimeStamp, ID: last.ID}
	}

	return &domain.LogListResult{
		Items:      items,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

// GetBySessionID returns all logs for a session, ordered by timestamp.
func (r *LogRepo) GetBySessionID(ctx context.Context, projectID, sessionID uuid.UUID, limit int) ([]domain.Log, error) {
	log := cslog.L(ctx)
	log.WithField("session_id", sessionID).Debug("DB: GetBySessionID")

	if limit <= 0 {
		limit = 500
	}

	db := getDB(ctx, r.db)
	var models []LogModel
	err := db.WithContext(ctx).
		Where("project_id = ? AND session_id = ?", projectID, sessionID).
		Order("time_stamp ASC").
		Limit(limit).
		Find(&models).Error
	if err != nil {
		log.WithError(err).Error("DB: GetBySessionID failed")
		return nil, err
	}

	items := make([]domain.Log, 0, len(models))
	for _, m := range models {
		items = append(items, *LogModelToDomain(&m))
	}
	return items, nil
}

// GetByRequestID returns all log phases for a network request (request/response/error).
func (r *LogRepo) GetByRequestID(ctx context.Context, projectID uuid.UUID, requestID uuid.UUID) ([]domain.Log, error) {
	log := cslog.L(ctx)
	log.WithField("request_id", requestID).Debug("DB: GetByRequestID")

	db := getDB(ctx, r.db)
	var models []LogModel
	err := db.WithContext(ctx).
		Where("project_id = ? AND request_id = ?", projectID, requestID).
		Order("time_stamp ASC").
		Find(&models).Error
	if err != nil {
		log.WithError(err).Error("DB: GetByRequestID failed")
		return nil, err
	}

	items := make([]domain.Log, 0, len(models))
	for _, m := range models {
		items = append(items, *LogModelToDomain(&m))
	}
	return items, nil
}

// GetStats returns hourly log count buckets for sparkline display.
func (r *LogRepo) GetStats(ctx context.Context, opts domain.LogStatsOpts) (*domain.LogStatsResult, error) {
	log := cslog.L(ctx)
	log.WithField("project_id", opts.ProjectID).Debug("DB: GetStats")

	hours := opts.Hours
	if hours <= 0 {
		hours = 24
	}

	db := getDB(ctx, r.db)
	cutoff := time.Now().Add(-time.Duration(hours) * time.Hour)

	type bucketRow struct {
		Hour       time.Time
		TotalCount int64
		ErrorCount int64
	}

	var rows []bucketRow
	err := db.WithContext(ctx).
		Model(&LogModel{}).
		Select(`
			DATE_FORMAT(time_stamp, '%Y-%m-%d %H:00:00') as hour,
			COUNT(*) as total_count,
			SUM(CASE WHEN level IN ('error', 'fatal') THEN 1 ELSE 0 END) as error_count
		`).
		Where("project_id = ? AND time_stamp >= ?", opts.ProjectID, cutoff).
		Group("hour").
		Order("hour ASC").
		Scan(&rows).Error
	if err != nil {
		log.WithError(err).Error("DB: GetStats failed")
		return nil, err
	}

	buckets := make([]domain.LogStatsBucket, 0, len(rows))
	var totalLogs, totalErrors int64
	for _, row := range rows {
		buckets = append(buckets, domain.LogStatsBucket{
			Hour:       row.Hour,
			TotalCount: row.TotalCount,
			ErrorCount: row.ErrorCount,
		})
		totalLogs += row.TotalCount
		totalErrors += row.ErrorCount
	}

	return &domain.LogStatsResult{
		Buckets:     buckets,
		TotalLogs:   totalLogs,
		TotalErrors: totalErrors,
	}, nil
}

// SoftDeleteBefore soft-deletes logs older than the given timestamp across all
// projects. Per-project retention can reintroduce a projectID filter when
// project-level settings exist.
func (r *LogRepo) SoftDeleteBefore(ctx context.Context, before time.Time) (int64, error) {
	log := cslog.L(ctx)
	log.WithField("before", before).Debug("DB: SoftDeleteBefore")

	db := getDB(ctx, r.db)
	result := db.WithContext(ctx).
		Where("time_stamp < ?", before).
		Delete(&LogModel{})
	if result.Error != nil {
		log.WithError(result.Error).Error("DB: SoftDeleteBefore failed")
		return 0, result.Error
	}

	log.WithField("count", result.RowsAffected).Info(fmt.Sprintf("DB: Soft-deleted %d logs", result.RowsAffected))
	return result.RowsAffected, nil
}

// PurgeSoftDeleted permanently removes soft-deleted logs older than the given time.
func (r *LogRepo) PurgeSoftDeleted(ctx context.Context, olderThan time.Time) (int64, error) {
	log := cslog.L(ctx)
	log.Debug("DB: PurgeSoftDeleted")

	db := getDB(ctx, r.db)
	result := db.WithContext(ctx).
		Unscoped().
		Where("deleted_at IS NOT NULL AND deleted_at < ?", olderThan).
		Delete(&LogModel{})
	if result.Error != nil {
		log.WithError(result.Error).Error("DB: PurgeSoftDeleted failed")
		return 0, result.Error
	}

	log.WithField("count", result.RowsAffected).Info(fmt.Sprintf("DB: Purged %d soft-deleted logs", result.RowsAffected))
	return result.RowsAffected, nil
}
