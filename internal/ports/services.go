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
}

type LogManager interface {
	DumpLogs(ctx context.Context, project *domain.Project, tr *tar.Reader) (int, error)
}
