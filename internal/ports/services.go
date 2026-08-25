package ports

import (
	"archive/tar"
	"context"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/google/uuid"
)

type ProjectManager interface {
	CreateProject(ctx context.Context, opts *domain.CreateProjectOpts) (*domain.ProjectDetails, int, error)
	GetProject(ctx context.Context, projectID uuid.UUID) (*domain.Project, int, error)
	UpdateProject(ctx context.Context, projectID uuid.UUID, opts *domain.UpdateProjectOpts) (*domain.Project, int, error)
	DeleteProject(ctx context.Context, projectID uuid.UUID) (int, error)
	RevealSecret(ctx context.Context, projectID uuid.UUID) (string, int, error)
	RotateSecret(ctx context.Context, projectID uuid.UUID) (string, int, error)
	ListProjects(ctx context.Context, opts domain.ProjectListOpts) (*domain.ProjectListResult, int, error)
	ToggleFavorite(ctx context.Context, userID, projectID uuid.UUID) (bool, int, error)
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
	// ChangePassword sets a signed-in account's own password and returns the
	// replacement session token, since changing it drops every existing session.
	ChangePassword(ctx context.Context, user *domain.User, current, next, confirm string) (string, int, error)
}
