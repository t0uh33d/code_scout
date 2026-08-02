package db

import (
	"time"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
	"gorm.io/gorm"
)

func toGormDeletedAt(t *time.Time) gorm.DeletedAt {
	if t != nil {
		return gorm.DeletedAt{Time: *t, Valid: true}
	}
	return gorm.DeletedAt{}
}

func fromGormDeletedAt(d gorm.DeletedAt) *time.Time {
	if d.Valid {
		return &d.Time
	}
	return nil
}

func ProjectModelToDomain(m *ProjectModel) *domain.Project {
	return &domain.Project{
		ID:          m.ID,
		Name:        m.Name,
		Description: m.Description,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
		DeletedAt:   fromGormDeletedAt(m.DeletedAt),
	}
}

func ProjectDomainToModel(p *domain.Project) *ProjectModel {
	return &ProjectModel{
		GormBase: GormBase{
			ID:        p.ID,
			CreatedAt: p.CreatedAt,
			UpdatedAt: p.UpdatedAt,
			DeletedAt: toGormDeletedAt(p.DeletedAt),
		},
		Name:        p.Name,
		Description: p.Description,
	}
}

func ProjectSecretModelToDomain(m *ProjectSecretModel) *domain.ProjectSecret {
	return &domain.ProjectSecret{
		ID:        m.ID,
		ProjectID: m.ProjectID,
		SecretKey: m.SecretKey,
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
		DeletedAt: fromGormDeletedAt(m.DeletedAt),
	}
}

func ProjectSecretDomainToModel(s *domain.ProjectSecret) *ProjectSecretModel {
	return &ProjectSecretModel{
		GormBase: GormBase{
			ID:        s.ID,
			CreatedAt: s.CreatedAt,
			UpdatedAt: s.UpdatedAt,
			DeletedAt: toGormDeletedAt(s.DeletedAt),
		},
		ProjectID: s.ProjectID,
		SecretKey: s.SecretKey,
	}
}

func LogDomainToModel(l *domain.Log) *LogModel {
	return &LogModel{
		GormBase: GormBase{
			ID:        l.ID,
			CreatedAt: l.CreatedAt,
			UpdatedAt: l.UpdatedAt,
			DeletedAt: toGormDeletedAt(l.DeletedAt),
		},
		ClientID:      l.ClientID,
		ProjectID:     l.ProjectID,
		SessionID:     l.SessionID,
		Level:         l.Level,
		Message:       l.Message,
		Error:         l.Error,
		StackTrace:    l.StackTrace,
		Metadata:      l.Metadata,
		Tags:          l.Tags,
		TimeStamp:     l.TimeStamp,
		IsNetworkCall: l.IsNetworkCall,
		RequestID:     l.RequestID,
		CallPhase:     l.CallPhase,
		Method:        l.Method,
		URL:           l.URL,
		StatusCode:    l.StatusCode,
	}
}

func LogModelToDomain(m *LogModel) *domain.Log {
	return &domain.Log{
		ID:            m.ID,
		ClientID:      m.ClientID,
		ProjectID:     m.ProjectID,
		SessionID:     m.SessionID,
		Level:         m.Level,
		Message:       m.Message,
		Error:         m.Error,
		StackTrace:    m.StackTrace,
		Metadata:      m.Metadata,
		Tags:          m.Tags,
		TimeStamp:     m.TimeStamp,
		IsNetworkCall: m.IsNetworkCall,
		RequestID:     m.RequestID,
		CallPhase:     m.CallPhase,
		Method:        m.Method,
		URL:           m.URL,
		StatusCode:    m.StatusCode,
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
		DeletedAt:     fromGormDeletedAt(m.DeletedAt),
	}
}

func UserModelToDomain(m *UserModel) *domain.User {
	return &domain.User{
		ID:                 m.ID,
		Name:               m.Name,
		Email:              m.Email,
		Role:               domain.Role(m.Role),
		PasswordHash:       m.PasswordHash,
		MustChangePassword: m.MustChangePassword,
		CreatedAt:          m.CreatedAt,
		UpdatedAt:          m.UpdatedAt,
		DeletedAt:          fromGormDeletedAt(m.DeletedAt),
	}
}

func UserDomainToModel(u *domain.User) *UserModel {
	return &UserModel{
		GormBase: GormBase{
			ID:        u.ID,
			CreatedAt: u.CreatedAt,
			UpdatedAt: u.UpdatedAt,
			DeletedAt: toGormDeletedAt(u.DeletedAt),
		},
		Name:               u.Name,
		Email:              u.Email,
		Role:               string(u.Role),
		PasswordHash:       u.PasswordHash,
		MustChangePassword: u.MustChangePassword,
	}
}

func ProjectMemberModelToDomain(m *ProjectMemberModel) *domain.ProjectMember {
	return &domain.ProjectMember{
		ID:        m.ID,
		UserID:    m.UserID,
		ProjectID: m.ProjectID,
		Level:     domain.ProjectLevel(m.Level),
		CreatedAt: m.CreatedAt,
	}
}

func UserSessionModelToDomain(m *UserSessionModel, userID string) *domain.UserSession {
	uid, _ := uuid.Parse(userID)
	return &domain.UserSession{
		ID:        m.ID,
		UserID:    uid,
		Token:     m.Token,
		ExpiresAt: m.ExpiresAt,
		CreatedAt: m.CreatedAt,
	}
}

func UserSessionDomainToModel(s *domain.UserSession) *UserSessionModel {
	return &UserSessionModel{
		GormBase: GormBase{
			ID:        s.ID,
			CreatedAt: s.CreatedAt,
		},
		UserID:    s.UserID.String(),
		Token:     s.Token,
		ExpiresAt: s.ExpiresAt,
	}
}
