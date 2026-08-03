package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SessionRepo struct {
	db *gorm.DB
}

func NewSessionRepo(db *gorm.DB) *SessionRepo {
	return &SessionRepo{db: db}
}

// Upsert records a session, or updates the one already there.
//
// The SDK re-sends this with every batch, so the repeat is the normal case, not
// an error. What it must never do is move started_at: the first record is the
// truth about when the app launched, and a later batch saying "now" would make
// every session look like it began at its last sync.
//
// project_id is likewise fixed on insert. It comes from the authenticated
// headers, so a client cannot move a session between projects by re-sending it.
func (r *SessionRepo) Upsert(ctx context.Context, session *domain.Session) error {
	log := cslog.L(ctx)

	model := SessionModel{
		ID:             session.ID,
		ProjectID:      session.ProjectID,
		InstallationID: session.InstallationID,
		UserID:         session.UserID,
		DeviceModel:    session.DeviceModel,
		OSName:         session.OSName,
		OSVersion:      session.OSVersion,
		AppVersion:     session.AppVersion,
		BuildNumber:    session.BuildNumber,
		Metadata:       session.Metadata,
		StartedAt:      session.StartedAt,
		LastSeenAt:     session.LastSeenAt,
	}

	db := getDB(ctx, r.db)
	err := db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			// Everything a later batch can legitimately revise. setUser() may
			// be called mid-session, and the device details can arrive on the
			// second batch if the first was sent before they resolved.
			"installation_id", "user_id",
			"device_model", "os_name", "os_version", "app_version", "build_number",
			"metadata", "last_seen_at", "updated_at",
		}),
	}).Create(&model).Error
	if err != nil {
		log.WithError(err).Error("DB: session upsert failed")
		return err
	}
	return nil
}

// GetByID returns one session, scoped to its project so an id from another
// project cannot be read by guessing.
func (r *SessionRepo) GetByID(ctx context.Context, projectID, sessionID uuid.UUID) (*domain.Session, error) {
	db := getDB(ctx, r.db)

	var model SessionModel
	err := db.WithContext(ctx).
		Where("project_id = ? AND id = ?", projectID, sessionID).
		First(&model).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return sessionModelToDomain(&model), nil
}

// logCounts is one row per session: how much it logged, and how much of that
// went wrong. Written once and joined from both list queries, so the Sessions
// screen and the Devices screen can never disagree about a number.
//
// A subquery rather than a join straight onto logs: aggregating first means one
// row per session meets the sessions table, where joining raw would multiply
// every session by its logs before grouping them back down.
const logCounts = `(
	SELECT session_id,
		COUNT(*) AS logs,
		COUNT(*) FILTER (WHERE level IN ('error', 'fatal')) AS errors,
		COUNT(*) FILTER (WHERE is_network_call) AS network
	FROM logs
	WHERE project_id = ? AND deleted_at IS NULL
	GROUP BY session_id
)`

// sessionRow is the flat shape both list queries scan into.
type sessionRow struct {
	SessionModel
	Logs    int64
	Errors  int64
	Network int64
}

func (r sessionRow) toDomain() domain.SessionSummary {
	return domain.SessionSummary{
		Session: *sessionModelToDomain(&r.SessionModel),
		Logs:    r.Logs,
		Errors:  r.Errors,
		Network: r.Network,
	}
}

// List returns sessions newest first. installationID narrows it to one device,
// which is the only thing the device detail screen needs beyond this.
func (r *SessionRepo) List(ctx context.Context, projectID uuid.UUID, installationID *uuid.UUID, limit int) ([]domain.SessionSummary, error) {
	log := cslog.L(ctx)

	if limit <= 0 {
		limit = 100
	}

	db := getDB(ctx, r.db)
	query := db.WithContext(ctx).
		Table("sessions AS s").
		Select(`s.*,
			COALESCE(c.logs, 0) AS logs,
			COALESCE(c.errors, 0) AS errors,
			COALESCE(c.network, 0) AS network`).
		Joins("LEFT JOIN "+logCounts+" AS c ON c.session_id = s.id", projectID).
		Where("s.project_id = ?", projectID)
	if installationID != nil {
		query = query.Where("s.installation_id = ?", *installationID)
	}

	var rows []sessionRow
	if err := query.Order("s.started_at DESC").Limit(limit).Scan(&rows).Error; err != nil {
		log.WithError(err).Error("DB: list sessions failed")
		return nil, err
	}

	out := make([]domain.SessionSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toDomain())
	}
	return out, nil
}

// Counts is the pair the Sessions toolbar shows: how many launches, and how
// many distinct people. Separate from List because the list is capped and these
// are about everything.
func (r *SessionRepo) Counts(ctx context.Context, projectID uuid.UUID) (sessions int64, users int64, err error) {
	db := getDB(ctx, r.db)

	var row struct {
		Sessions int64
		Users    int64
	}
	err = db.WithContext(ctx).
		Model(&SessionModel{}).
		Select("COUNT(*) AS sessions, COUNT(DISTINCT user_id) AS users").
		Where("project_id = ?", projectID).
		Scan(&row).Error
	if err != nil {
		cslog.L(ctx).WithError(err).Error("DB: session counts failed")
		return 0, 0, err
	}
	return row.Sessions, row.Users, nil
}

// deviceRow is one install: the newest session's answer for each detail, and
// totals across all of them.
type deviceRow struct {
	InstallationID uuid.UUID
	DeviceModel    *string
	OSName         *string
	OSVersion      *string
	AppVersion     *string
	BuildNumber    *string
	LastUserID     *string
	Sessions       int64
	Logs           int64
	Errors         int64
	FirstSeen      time.Time
	LastSeen       time.Time
}

// deviceSelect rolls sessions up per install.
//
// Each detail is the most recent session's, so a phone that has been updated
// reads as what it is now rather than what it first was. The user is the most
// recent session that actually had one — FILTER skips the anonymous launches
// in between, so a device does not forget who was on it because the last
// launch never called setUser().
const deviceSelect = `s.installation_id,
	(array_agg(s.device_model ORDER BY s.started_at DESC))[1] AS device_model,
	(array_agg(s.os_name     ORDER BY s.started_at DESC))[1] AS os_name,
	(array_agg(s.os_version  ORDER BY s.started_at DESC))[1] AS os_version,
	(array_agg(s.app_version ORDER BY s.started_at DESC))[1] AS app_version,
	(array_agg(s.build_number ORDER BY s.started_at DESC))[1] AS build_number,
	(array_agg(s.user_id ORDER BY s.started_at DESC) FILTER (WHERE s.user_id IS NOT NULL))[1] AS last_user_id,
	COUNT(*) AS sessions,
	COALESCE(SUM(c.logs), 0) AS logs,
	COALESCE(SUM(c.errors), 0) AS errors,
	MIN(s.started_at) AS first_seen,
	MAX(s.last_seen_at) AS last_seen`

func (r deviceRow) toDomain() domain.Device {
	return domain.Device{
		InstallationID: r.InstallationID,
		DeviceModel:    r.DeviceModel,
		OSName:         r.OSName,
		OSVersion:      r.OSVersion,
		AppVersion:     r.AppVersion,
		BuildNumber:    r.BuildNumber,
		LastUserID:     r.LastUserID,
		Sessions:       r.Sessions,
		Logs:           r.Logs,
		Errors:         r.Errors,
		FirstSeen:      r.FirstSeen,
		LastSeen:       r.LastSeen,
	}
}

// ListDevices returns every install that has reported, most recently seen
// first. Sessions with no installation id are skipped rather than collapsed
// into one nameless row: they came from an SDK that does not send one, and
// pretending they are all the same phone would be a lie.
func (r *SessionRepo) ListDevices(ctx context.Context, projectID uuid.UUID, limit int) ([]domain.Device, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := r.devices(ctx, projectID, nil, limit)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Device, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toDomain())
	}
	return out, nil
}

// GetDevice is one install.
func (r *SessionRepo) GetDevice(ctx context.Context, projectID, installationID uuid.UUID) (*domain.Device, error) {
	rows, err := r.devices(ctx, projectID, &installationID, 1)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, domain.ErrNotFound
	}
	device := rows[0].toDomain()
	return &device, nil
}

func (r *SessionRepo) devices(ctx context.Context, projectID uuid.UUID, installationID *uuid.UUID, limit int) ([]deviceRow, error) {
	db := getDB(ctx, r.db)
	query := db.WithContext(ctx).
		Table("sessions AS s").
		Select(deviceSelect).
		Joins("LEFT JOIN "+logCounts+" AS c ON c.session_id = s.id", projectID).
		Where("s.project_id = ? AND s.installation_id IS NOT NULL", projectID)
	if installationID != nil {
		query = query.Where("s.installation_id = ?", *installationID)
	}

	var rows []deviceRow
	if err := query.Group("s.installation_id").Order("last_seen DESC").Limit(limit).Scan(&rows).Error; err != nil {
		cslog.L(ctx).WithError(err).Error("DB: list devices failed")
		return nil, err
	}
	return rows, nil
}

func sessionModelToDomain(m *SessionModel) *domain.Session {
	return &domain.Session{
		ID:             m.ID,
		ProjectID:      m.ProjectID,
		InstallationID: m.InstallationID,
		UserID:         m.UserID,
		DeviceModel:    m.DeviceModel,
		OSName:         m.OSName,
		OSVersion:      m.OSVersion,
		AppVersion:     m.AppVersion,
		BuildNumber:    m.BuildNumber,
		Metadata:       m.Metadata,
		StartedAt:      m.StartedAt,
		LastSeenAt:     m.LastSeenAt,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
	}
}
