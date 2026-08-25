package db

import (
	"context"
	"time"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"github.com/google/uuid"
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

	// Clamped to the column widths. These are free-form strings from whatever
	// device the app is on, and one of them being long must not cost the whole
	// session — which is exactly what happened: Linux reports an OS version of
	// "#26~22.04.1-Ubuntu SMP PREEMPT_DYNAMIC ..." where macOS reports
	// "Version 26.5.2 (Build 25F84)", so a Linux launch tripped
	// varchar(64) and vanished, logs and all context with it.
	//
	// Truncating rather than widening: there is no length that some device
	// cannot exceed, and these are shown to a person, not parsed. Losing the
	// tail of a kernel banner costs nothing; losing the session costs the user,
	// the device and the app version it was carrying.
	model := SessionModel{
		ID:             session.ID,
		ProjectID:      session.ProjectID,
		InstallationID: session.InstallationID,
		UserID:         clamp(session.UserID, 255),
		DeviceModel:    clamp(session.DeviceModel, 255),
		OSName:         clamp(session.OSName, 64),
		OSVersion:      clamp(session.OSVersion, 64),
		AppVersion:     clamp(session.AppVersion, 64),
		BuildNumber:    clamp(session.BuildNumber, 64),
		SDKVersion:     clamp(session.SDKVersion, 64),
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
			// Without this an app that upgrades its SDK keeps reporting the old
			// version forever on any session row that already exists.
			"sdk_version",
			"metadata", "last_seen_at", "updated_at",
		}),
		// The session id is the client's own, so it is attacker-chosen. Without
		// this the conflict target is the primary key alone, and anyone holding
		// any project's credentials could name another project's session id and
		// have this statement rewrite its user, device, app version and
		// metadata. Not a read of the other project, but a write into it.
		//
		// Scoped in the WHERE rather than by widening the key to
		// (project_id, id): a colliding id then updates nothing instead of
		// raising, which is the right answer for an upsert that runs on every
		// batch. A genuine v4/v7 collision across two projects is not a thing
		// that happens by accident.
		Where: clause.Where{Exprs: []clause.Expression{
			clause.Eq{
				Column: clause.Column{Table: "sessions", Name: "project_id"},
				Value:  session.ProjectID,
			},
		}},
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
		SDKVersion:     m.SDKVersion,
		Metadata:       m.Metadata,
		StartedAt:      m.StartedAt,
		LastSeenAt:     m.LastSeenAt,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
	}
}

// PurgeSessionsBefore drops sessions last seen before the cutoff.
//
// The cutoff is deliberately the same one that purges logs for good, not the
// one that soft-deletes them. A session is what a log's session_id points at,
// and what every session-scoped search term resolves through, so removing it
// while any of its logs survive would leave those logs unattributable and drop
// them out of `user:`, `device:` and `app_version:` results without deleting
// them. Once the logs are gone the row describes nothing.
func (r *SessionRepo) PurgeSessionsBefore(ctx context.Context, before time.Time) (int64, error) {
	db := getDB(ctx, r.db)
	result := db.WithContext(ctx).
		Where("last_seen_at < ?", before).
		Delete(&SessionModel{})
	if result.Error != nil {
		cslog.L(ctx).WithError(result.Error).Error("DB: purge sessions failed")
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// PurgeOrphanedSessions drops sessions whose project has been deleted, in
// batches, exactly as PurgeOrphanedLogs does and for the same reason: deleting
// a project is O(1) on purpose, so its rows are reaped by the nightly job
// rather than inline.
func (r *SessionRepo) PurgeOrphanedSessions(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}

	db := getDB(ctx, r.db)
	// Postgres has no DELETE ... LIMIT, so the physical row ids come first.
	result := db.WithContext(ctx).Exec(`
		DELETE FROM sessions WHERE ctid IN (
			SELECT s.ctid FROM sessions s
			JOIN projects p ON p.id = s.project_id
			WHERE p.deleted_at IS NOT NULL
			LIMIT ?
		)`, limit)
	if result.Error != nil {
		cslog.L(ctx).WithError(result.Error).Error("DB: PurgeOrphanedSessions failed")
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// clamp cuts a string to at most n characters, counting runes rather than
// bytes: Postgres counts characters too, and cutting mid-rune would store
// broken UTF-8 for the sake of a limit that was not actually reached.
func clamp(v *string, n int) *string {
	if v == nil {
		return nil
	}
	r := []rune(*v)
	if len(r) <= n {
		return v
	}
	cut := string(r[:n])
	return &cut
}
