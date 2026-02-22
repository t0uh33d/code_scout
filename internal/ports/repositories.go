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

type UserRepository interface {
	Count(ctx context.Context, tx *gorm.DB) (int64, error)
	GetByUsername(ctx context.Context, tx *gorm.DB, username string) (*domain.User, error)
	Create(ctx context.Context, tx *gorm.DB, user *domain.User) error
	CreateSession(ctx context.Context, tx *gorm.DB, session *domain.UserSession) error
	GetSessionByToken(ctx context.Context, tx *gorm.DB, token string) (*domain.UserSession, error)
	DeleteSession(ctx context.Context, tx *gorm.DB, token string) error
}
