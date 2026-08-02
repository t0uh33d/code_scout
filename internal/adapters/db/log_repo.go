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
	"gorm.io/gorm/clause"
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

	// Ingestion is idempotent on (project_id, client_id). If an upload commits
	// but the response is lost, the SDK retries the same batch and the repeated
	// rows are skipped rather than inserted twice. Rows with no client id have
	// a null there and are always inserted.
	result := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "project_id"}, {Name: "client_id"}},
		DoNothing: true,
	}).CreateInBatches(models, 100)
	if result.Error != nil {
		log.WithError(result.Error).Error("DB: CreateBatch failed")
		return result.Error
	}

	if skipped := int64(len(models)) - result.RowsAffected; skipped > 0 {
		log.WithField("skipped", skipped).Info("DB: skipped already-ingested logs")
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
	// OR across levels: the toolbar's toggles are "show me these kinds".
	if len(f.Levels) > 0 {
		query = query.Where("level IN ?", f.Levels)
	}
	if f.Since != nil {
		query = query.Where("time_stamp >= ?", *f.Since)
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
	if f.Fingerprint != nil {
		query = query.Where("fingerprint = ?", *f.Fingerprint)
	}
	if f.TextQuery != "" {
		query = query.Where("message LIKE ?", "%"+f.TextQuery+"%")
	}
	if len(f.Tags) > 0 {
		for _, tag := range f.Tags {
			// jsonb containment, served by the GIN index on tags rather than
			// scanning every row the way a JSON function would.
			tagJSON, _ := json.Marshal([]string{tag})
			query = query.Where("tags @> ?::jsonb", string(tagJSON))
		}
	}
	for _, tag := range f.ExcludeTags {
		// NOT of the same containment test. A log with no tags at all has a
		// NULL column, and NOT NULL-containment is NULL rather than true, so
		// the COALESCE is what keeps untagged logs visible.
		tagJSON, _ := json.Marshal([]string{tag})
		query = query.Where("COALESCE(tags @> ?::jsonb, false) = false", string(tagJSON))
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
			date_trunc('hour', time_stamp) as hour,
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

// GetOverview counts everything the overview screen shows.
//
// Two queries, not five: one hourly rollup over 48 hours which the caller
// splits into today and yesterday, and one distinct-session count. Sessions
// cannot come from the rollup because distinct counts do not sum across
// buckets — the same session appears in every hour it logged in.
func (r *LogRepo) GetOverview(ctx context.Context, projectID uuid.UUID) (*domain.ProjectOverview, error) {
	log := cslog.L(ctx)
	log.WithField("project_id", projectID).Debug("DB: GetOverview")

	db := getDB(ctx, r.db)
	// Everything here is UTC on purpose. time.Truncate works on absolute time,
	// so truncating a +05:30 clock to the hour lands on :30 and would never
	// match date_trunc's :00 — the buckets would silently all miss.
	now := time.Now().UTC().Truncate(time.Hour)
	windowStart := now.Add(-23 * time.Hour)
	cutoff := now.Add(-47 * time.Hour)

	type bucketRow struct {
		Hour         time.Time
		TotalCount   int64
		ErrorCount   int64
		NetworkCount int64
		FailedCount  int64
	}

	var rows []bucketRow
	err := db.WithContext(ctx).
		Model(&LogModel{}).
		Select(`
			date_trunc('hour', time_stamp AT TIME ZONE 'UTC') as hour,
			COUNT(*) as total_count,
			SUM(CASE WHEN level IN ('error', 'fatal') THEN 1 ELSE 0 END) as error_count,
			SUM(CASE WHEN is_network_call THEN 1 ELSE 0 END) as network_count,
			SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END) as failed_count
		`).
		Where("project_id = ? AND time_stamp >= ?", projectID, cutoff).
		Group("hour").
		Order("hour ASC").
		Scan(&rows).Error
	if err != nil {
		log.WithError(err).Error("DB: GetOverview rollup failed")
		return nil, err
	}

	out := &domain.ProjectOverview{WindowStartHour: windowStart}
	// One entry per hour whether or not anything was logged, so the chart has a
	// steady 24 columns rather than collapsing quiet hours.
	byHour := make(map[time.Time]bucketRow, len(rows))
	for _, row := range rows {
		bucketHour := row.Hour.UTC().Truncate(time.Hour)
		byHour[bucketHour] = row
		if bucketHour.Before(windowStart) {
			out.PrevLogs += row.TotalCount
			out.PrevErrors += row.ErrorCount
		}
	}
	for i := 0; i < 24; i++ {
		hour := windowStart.Add(time.Duration(i) * time.Hour)
		row := byHour[hour]
		out.Buckets = append(out.Buckets, domain.LogStatsBucket{
			Hour:         hour,
			TotalCount:   row.TotalCount,
			ErrorCount:   row.ErrorCount,
			NetworkCount: row.NetworkCount,
			FailedCount:  row.FailedCount,
		})
		out.Logs += row.TotalCount
		out.Errors += row.ErrorCount
		out.Network += row.NetworkCount
		out.Failed += row.FailedCount
		if row.ErrorCount > out.PeakErrorCount {
			out.PeakErrorCount = row.ErrorCount
			out.PeakErrorHour = hour
		}
	}

	if err := db.WithContext(ctx).
		Model(&LogModel{}).
		Where("project_id = ? AND time_stamp >= ?", projectID, windowStart).
		Distinct("session_id").
		Count(&out.Sessions).Error; err != nil {
		log.WithError(err).Error("DB: GetOverview session count failed")
		return nil, err
	}

	return out, nil
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

// PurgeOrphanedLogs hard-deletes up to limit log rows whose project has been
// deleted, returning how many went. Call again while it returns limit.
//
// Deleting a project's logs inline is unbounded work and would blow the
// server's write timeout on a busy project, rolling back the delete. So the
// project goes immediately and its logs are reaped here, in batches, by the
// nightly job.
func (r *LogRepo) PurgeOrphanedLogs(ctx context.Context, limit int) (int64, error) {
	log := cslog.L(ctx)

	if limit <= 0 {
		return 0, nil
	}

	db := getDB(ctx, r.db)
	// Postgres has no DELETE ... LIMIT, so select the physical row ids first.
	result := db.WithContext(ctx).Exec(`
		DELETE FROM logs WHERE ctid IN (
			SELECT l.ctid FROM logs l
			JOIN projects p ON p.id = l.project_id
			WHERE p.deleted_at IS NOT NULL
			LIMIT ?
		)`, limit)
	if result.Error != nil {
		log.WithError(result.Error).Error("DB: PurgeOrphanedLogs failed")
		return 0, result.Error
	}
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

// GetTagCounts returns the tags in use on a project, most used first, so the
// picker offers what actually exists rather than a free-text box.
//
// jsonb_array_elements_text unnests the tags array into one row per tag, which
// is the only way to group by something inside a JSON column. It cannot use the
// GIN index, so the window bound and the limit are what keep it cheap.
func (r *LogRepo) GetTagCounts(ctx context.Context, projectID uuid.UUID, since *time.Time, limit int) ([]domain.TagCount, error) {
	log := cslog.L(ctx)
	log.WithField("project_id", projectID).Debug("DB: GetTagCounts")

	if limit <= 0 {
		limit = 20
	}

	db := getDB(ctx, r.db)
	query := db.WithContext(ctx).
		Table("logs, jsonb_array_elements_text(logs.tags) AS tag").
		Select("tag AS tag, COUNT(*) AS count").
		Where("logs.project_id = ?", projectID).
		Where("logs.deleted_at IS NULL").
		// jsonb_array_elements_text errors on a non-array, and rows with no
		// tags at all have NULL rather than an empty array.
		Where("jsonb_typeof(logs.tags) = 'array'")
	if since != nil {
		query = query.Where("logs.time_stamp >= ?", *since)
	}

	var counts []domain.TagCount
	err := query.Group("tag").Order("count DESC, tag ASC").Limit(limit).Scan(&counts).Error
	if err != nil {
		log.WithError(err).Error("DB: GetTagCounts failed")
		return nil, err
	}
	return counts, nil
}

// GetErrorGroups collapses errors into distinct problems, most frequent first.
//
// Grouping is on the fingerprint computed at ingest, so this is an indexed
// GROUP BY rather than normalising every message on every read. Logs written
// before fingerprinting existed have none and are excluded — with no data to
// preserve, that is cheaper and more honest than a fallback that would group
// them by raw message and quietly behave differently.
func (r *LogRepo) GetErrorGroups(ctx context.Context, projectID uuid.UUID, since *time.Time, limit int) ([]domain.ErrorGroup, error) {
	log := cslog.L(ctx)
	log.WithField("project_id", projectID).Debug("DB: GetErrorGroups")

	if limit <= 0 {
		limit = 50
	}

	type row struct {
		Fingerprint     string
		SampleMessage   string
		Level           string
		Count           int64
		Sessions        int64
		FirstSeen       time.Time
		LastSeen        time.Time
		LatestLogID     uuid.UUID
		LatestSessionID uuid.UUID
		StackTrace      *json.RawMessage
		Tags            *json.RawMessage
	}

	db := getDB(ctx, r.db)
	query := db.WithContext(ctx).
		Model(&LogModel{}).
		Select(`
			fingerprint,
			COUNT(*) AS count,
			COUNT(DISTINCT session_id) AS sessions,
			MIN(time_stamp) AS first_seen,
			MAX(time_stamp) AS last_seen,
			(array_agg(message ORDER BY time_stamp DESC))[1] AS sample_message,
			(array_agg(level ORDER BY time_stamp DESC))[1] AS level,
			(array_agg(id ORDER BY time_stamp DESC))[1] AS latest_log_id,
			(array_agg(session_id ORDER BY time_stamp DESC))[1] AS latest_session_id,
			(array_agg(stack_trace ORDER BY time_stamp DESC))[1] AS stack_trace,
			(array_agg(tags ORDER BY time_stamp DESC))[1] AS tags
		`).
		Where("project_id = ? AND fingerprint IS NOT NULL", projectID).
		Where("deleted_at IS NULL")
	if since != nil {
		query = query.Where("time_stamp >= ?", *since)
	}

	var rows []row
	if err := query.Group("fingerprint").Order("count DESC, last_seen DESC").Limit(limit).Scan(&rows).Error; err != nil {
		log.WithError(err).Error("DB: GetErrorGroups failed")
		return nil, err
	}

	groups := make([]domain.ErrorGroup, 0, len(rows))
	for _, r := range rows {
		groups = append(groups, domain.ErrorGroup(r))
	}
	return groups, nil
}
