package ports

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
)

type ProjectRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error)
	GetByName(ctx context.Context, name string) (*domain.Project, error)
	Create(ctx context.Context, project *domain.Project) error
	Delete(ctx context.Context, project *domain.Project) error
	GetSecret(ctx context.Context, projectID uuid.UUID) (*domain.ProjectSecret, error)
	CreateSecret(ctx context.Context, secret *domain.ProjectSecret) error
	DeleteSecret(ctx context.Context, secret *domain.ProjectSecret) error
	List(ctx context.Context, opts domain.ProjectListOpts) (*domain.ProjectListResult, error)
}

type LogRepository interface {
	CreateBatch(ctx context.Context, logs []domain.Log) error
	List(ctx context.Context, opts domain.LogListOpts) (*domain.LogListResult, error)
	GetBySessionID(ctx context.Context, projectID, sessionID uuid.UUID, limit int) ([]domain.Log, error)
	GetByRequestID(ctx context.Context, projectID uuid.UUID, requestID uuid.UUID) ([]domain.Log, error)
	GetStats(ctx context.Context, opts domain.LogStatsOpts) (*domain.LogStatsResult, error)
	SoftDeleteBefore(ctx context.Context, before time.Time) (int64, error)
	PurgeSoftDeleted(ctx context.Context, olderThan time.Time) (int64, error)
}

type UserRepository interface {
	Count(ctx context.Context) (int64, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	Create(ctx context.Context, user *domain.User) error
	CreateSession(ctx context.Context, session *domain.UserSession) error
	GetSessionByToken(ctx context.Context, token string) (*domain.UserSession, error)
	DeleteSession(ctx context.Context, token string) error
}
