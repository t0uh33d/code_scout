package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/pkg/cslog"
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
