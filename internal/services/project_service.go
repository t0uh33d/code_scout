package services

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/internal/ports"
	"github.com/t0uh33d/code_scout/pkg/cslog"
	"github.com/t0uh33d/code_scout/pkg/utils"
)

type ProjectService struct {
	repo  ports.ProjectRepository
	txMgr ports.TransactionManager
}

func NewProjectService(repo ports.ProjectRepository, txMgr ports.TransactionManager) *ProjectService {
	return &ProjectService{
		repo:  repo,
		txMgr: txMgr,
	}
}

func (s *ProjectService) CreateProject(ctx context.Context, opts *domain.CreateProjectOpts) (*domain.ProjectDetails, int, error) {
	log := cslog.L(ctx)

	log.WithField("name", opts.Name).Info("Creating new project")
	if err := s.validateCreateProjectOpts(ctx, opts, nil); err != nil {
		log.Error("Validation failed for creating project")
		appErr := utils.NewError(err, domain.ERR_FAILED_TO_CREATE_PROJECT_ERR_CODE,
			errors.New(domain.ERR_FAILED_TO_CREATE_PROJECT_ERR))
		return nil, http.StatusBadRequest, appErr
	}

	project := &domain.Project{
		Name:        opts.Name,
		Description: opts.Description,
	}

	var details *domain.ProjectDetails
	err := s.txMgr.WithTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repo.Create(txCtx, project); err != nil {
			return err
		}

		secret := &domain.ProjectSecret{
			ProjectID: project.ID.String(),
			SecretKey: utils.GenerateRandomString(32),
		}
		if err := s.repo.CreateSecret(txCtx, secret); err != nil {
			return err
		}

		details = &domain.ProjectDetails{
			ID:          project.ID,
			Name:        project.Name,
			Description: project.Description,
			SecretKey:   secret.SecretKey,
		}
		return nil
	})
	if err != nil {
		log.WithError(err).Error("Failed to create project")
		appErr := utils.NewError(nil, domain.ERR_FAILED_TO_CREATE_PROJECT_ERR_CODE,
			errors.New(domain.ERR_FAILED_TO_CREATE_PROJECT_ERR))
		return nil, http.StatusInternalServerError, appErr
	}

	log.WithField("id", project.ID).Info("Project created successfully")
	return details, http.StatusOK, nil
}

func (s *ProjectService) DeleteProject(ctx context.Context, projectID uuid.UUID) (int, error) {
	log := cslog.L(ctx)

	log.WithField("id", projectID).Info("Deleting project")

	err := s.txMgr.WithTransaction(ctx, func(txCtx context.Context) error {
		project, err := s.repo.GetByID(txCtx, projectID)
		if err != nil {
			return err
		}

		if err := s.repo.Delete(txCtx, project); err != nil {
			return err
		}

		secret, err := s.repo.GetSecret(txCtx, project.ID)
		if err != nil {
			return err
		}

		if secret != nil {
			if err := s.repo.DeleteSecret(txCtx, secret); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		log.WithError(err).Error("Failed to delete project")
		appErr := utils.NewError(nil, domain.ERR_FAILED_TO_DELETE_PROJECT_ERR_CODE,
			errors.New(domain.ERR_FAILED_TO_DELETE_PROJECT_ERR))
		return http.StatusInternalServerError, appErr
	}

	log.WithField("id", projectID).Info("Project deleted successfully")
	return http.StatusOK, nil
}

func (s *ProjectService) validateCreateProjectOpts(ctx context.Context, opts *domain.CreateProjectOpts, id *uuid.UUID) []utils.FieldError {
	attrErrs := []utils.FieldError{}

	if opts.Name == "" {
		err := utils.CreateFieldError(domain.ERR_INVALID_PROJECT_NAME_ERR_CODE,
			domain.ERR_INVALID_PROJECT_NAME_ERR, "name", "Project name cannot empty")
		attrErrs = append(attrErrs, err)
	}

	if len(attrErrs) > 0 {
		return attrErrs
	}

	tmp, err := s.repo.GetByName(ctx, opts.Name)
	if err == nil {
		if id != nil && tmp.ID == *id {
			return nil
		}

		fieldErr := utils.CreateFieldError(domain.ERR_INVALID_PROJECT_NAME_ERR_CODE,
			domain.ERR_INVALID_PROJECT_NAME_ERR, "name", "Project with a same name already exists")
		return []utils.FieldError{fieldErr}
	}

	return nil
}

func (s *ProjectService) ListProjects(ctx context.Context, opts domain.ProjectListOpts) (*domain.ProjectListResult, int, error) {
	log := cslog.L(ctx)
	log.Debug("Listing projects")

	result, err := s.repo.List(ctx, opts)
	if err != nil {
		log.WithError(err).Error("Failed to list projects")
		appErr := utils.NewError(nil, domain.ERR_FAILED_TO_CREATE_PROJECT_ERR_CODE,
			errors.New("Failed to list projects"))
		return nil, http.StatusInternalServerError, appErr
	}

	return result, http.StatusOK, nil
}

func (s *ProjectService) ValidateProjectCredentials(ctx context.Context, projectID uuid.UUID, secret string) (*domain.Project, int, error) {
	log := cslog.L(ctx)

	project, err := s.repo.GetByID(ctx, projectID)
	if err != nil {
		return nil, http.StatusBadRequest, utils.NewError(nil, domain.INVALID_PROJECT_ID_HEADER_ERR_CODE,
			errors.New(domain.INVALID_PROJECT_ID_HEADER_ERR))
	}

	dbSecret, err := s.repo.GetSecret(ctx, project.ID)
	if err != nil || dbSecret == nil || dbSecret.SecretKey != secret {
		return nil, http.StatusBadRequest, utils.NewError(nil, domain.INVALID_PROJECT_SECRET_HEADER_ERR_CODE,
			errors.New(domain.INVALID_PROJECT_SECRET_HEADER_ERR))
	}

	log.WithField("project_id", projectID).Debug("Project credentials validated")
	return project, http.StatusOK, nil
}
