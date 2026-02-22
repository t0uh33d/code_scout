package ports

import (
	"context"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
	"gorm.io/gorm"
)

type ProjectRepository interface {
	GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*domain.Project, error)
	GetByName(ctx context.Context, tx *gorm.DB, name string) (*domain.Project, error)
	Create(ctx context.Context, tx *gorm.DB, project *domain.Project) error
	Delete(ctx context.Context, tx *gorm.DB, project *domain.Project) error
	GetSecret(ctx context.Context, tx *gorm.DB, projectID uuid.UUID) (*domain.ProjectSecret, error)
	CreateSecret(ctx context.Context, tx *gorm.DB, secret *domain.ProjectSecret) error
	DeleteSecret(ctx context.Context, tx *gorm.DB, secret *domain.ProjectSecret) error
}

type LogRepository interface {
	CreateBatch(ctx context.Context, tx *gorm.DB, logs []domain.Log) error
}
