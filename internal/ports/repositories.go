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
	Update(ctx context.Context, project *domain.Project) error
	Delete(ctx context.Context, project *domain.Project) error
	// LockProject takes a row-level write lock. Only meaningful in a transaction.
	LockProject(ctx context.Context, id uuid.UUID) (*domain.Project, error)
	GetSecret(ctx context.Context, projectID uuid.UUID) (*domain.ProjectSecret, error)
	CreateSecret(ctx context.Context, secret *domain.ProjectSecret) error
	DeleteSecret(ctx context.Context, secret *domain.ProjectSecret) error
	ReplaceSecret(ctx context.Context, projectID uuid.UUID, secretKey string) (*domain.ProjectSecret, error)
	DeleteSecretsByProject(ctx context.Context, projectID uuid.UUID) (int64, error)
	DeleteFavoritesByProject(ctx context.Context, projectID uuid.UUID) (int64, error)
	List(ctx context.Context, opts domain.ProjectListOpts) (*domain.ProjectListResult, error)
	SetFavorite(ctx context.Context, userID, projectID uuid.UUID, favorite bool) error
	IsFavorite(ctx context.Context, userID, projectID uuid.UUID) (bool, error)
}

type LogRepository interface {
	CreateBatch(ctx context.Context, logs []domain.Log) error
	List(ctx context.Context, opts domain.LogListOpts) (*domain.LogListResult, error)
	GetBySessionID(ctx context.Context, projectID, sessionID uuid.UUID, limit int) ([]domain.Log, error)
	GetByRequestID(ctx context.Context, projectID uuid.UUID, requestID uuid.UUID) ([]domain.Log, error)
	GetStats(ctx context.Context, opts domain.LogStatsOpts) (*domain.LogStatsResult, error)
	SoftDeleteBefore(ctx context.Context, before time.Time) (int64, error)
	PurgeSoftDeleted(ctx context.Context, olderThan time.Time) (int64, error)
	// PurgeOrphanedLogs removes up to limit rows belonging to deleted projects.
	PurgeOrphanedLogs(ctx context.Context, limit int) (int64, error)
}

type UserRepository interface {
	Count(ctx context.Context) (int64, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	UpdatePasswordHash(ctx context.Context, userID uuid.UUID, hash string) error
	DeleteSessionsByUserID(ctx context.Context, userID uuid.UUID) error
	Create(ctx context.Context, user *domain.User) error
	CreateSession(ctx context.Context, session *domain.UserSession) error
	GetSessionByToken(ctx context.Context, token string) (*domain.UserSession, error)
	DeleteSession(ctx context.Context, token string) error
}
