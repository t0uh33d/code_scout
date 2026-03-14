package ports

import (
	"archive/tar"
	"context"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
)

type ProjectManager interface {
	CreateProject(ctx context.Context, opts *domain.CreateProjectOpts) (*domain.ProjectDetails, int, error)
	DeleteProject(ctx context.Context, projectID uuid.UUID) (int, error)
	ListProjects(ctx context.Context, opts domain.ProjectListOpts) (*domain.ProjectListResult, int, error)
	ValidateProjectCredentials(ctx context.Context, projectID uuid.UUID, secret string) (*domain.Project, int, error)
}

type LogManager interface {
	DumpLogs(ctx context.Context, project *domain.Project, tr *tar.Reader) (int, error)
}

type AuthManager interface {
	// LoginOrRegister creates a new user account (first run) or validates credentials (subsequent runs).
	// Returns (session_token, isNewUser, httpStatus, error).
	LoginOrRegister(ctx context.Context, opts *domain.AuthOpts) (string, bool, int, error)
	// Logout deletes the session identified by token.
	Logout(ctx context.Context, token string) (int, error)
	// ValidateSession checks the session token and returns the associated user.
	ValidateSession(ctx context.Context, token string) (*domain.User, int, error)
	// IsFirstRun returns true if no user has been registered yet.
	IsFirstRun(ctx context.Context) (bool, error)
}
