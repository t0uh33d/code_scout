package db

import (
	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/pkg/utils"
)

func ProjectModelToDomain(m *ProjectModel) *domain.Project {
	return &domain.Project{
		ID:          m.ID,
		Name:        m.Name,
		Description: m.Description,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
		DeletedAt:   m.DeletedAt,
	}
}

func ProjectDomainToModel(p *domain.Project) *ProjectModel {
	return &ProjectModel{
		GormBase: utils.GormBase{
			ID:        p.ID,
			CreatedAt: p.CreatedAt,
			UpdatedAt: p.UpdatedAt,
			DeletedAt: p.DeletedAt,
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
		DeletedAt: m.DeletedAt,
	}
}

func ProjectSecretDomainToModel(s *domain.ProjectSecret) *ProjectSecretModel {
	return &ProjectSecretModel{
		GormBase: utils.GormBase{
			ID:        s.ID,
			CreatedAt: s.CreatedAt,
			UpdatedAt: s.UpdatedAt,
			DeletedAt: s.DeletedAt,
		},
		ProjectID: s.ProjectID,
		SecretKey: s.SecretKey,
	}
}

func LogDomainToModel(l *domain.Log) *LogModel {
	return &LogModel{
		GormBase: utils.GormBase{
			ID:        l.ID,
			CreatedAt: l.CreatedAt,
			UpdatedAt: l.UpdatedAt,
			DeletedAt: l.DeletedAt,
		},
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
	}
}
