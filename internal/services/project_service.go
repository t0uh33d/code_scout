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
	"gorm.io/gorm"
)

type ProjectService struct {
	repo ports.ProjectRepository
	db   *gorm.DB
}

func NewProjectService(repo ports.ProjectRepository, db *gorm.DB) *ProjectService {
	return &ProjectService{
		repo: repo,
		db:   db,
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

	tx := s.db.Begin()
	if tx.Error != nil {
		log.WithError(tx.Error).Error("Failed to start transaction")
		appErr := utils.NewError(nil, domain.ERR_FAILED_TO_CREATE_PROJECT_ERR_CODE,
			errors.New(domain.ERR_FAILED_TO_CREATE_PROJECT_ERR))
		return nil, http.StatusInternalServerError, appErr
	}

	project := &domain.Project{
		Name:        opts.Name,
		Description: opts.Description,
	}
	if err := s.repo.Create(ctx, tx, project); err != nil {
		log.WithError(err).Error("Failed to create project")
		tx.Rollback()
		appErr := utils.NewError(nil, domain.ERR_FAILED_TO_CREATE_PROJECT_ERR_CODE,
			errors.New(domain.ERR_FAILED_TO_CREATE_PROJECT_ERR))
		return nil, http.StatusInternalServerError, appErr
	}

	secret := &domain.ProjectSecret{
		ProjectID: project.ID.String(),
		SecretKey: utils.GenerateRandomString(32),
	}
	if err := s.repo.CreateSecret(ctx, tx, secret); err != nil {
		log.WithError(err).Error("Failed to create project secret")
		tx.Rollback()
		appErr := utils.NewError(nil, domain.ERR_FAILED_TO_CREATE_PROJECT_ERR_CODE,
			errors.New(domain.ERR_FAILED_TO_CREATE_PROJECT_ERR))
		return nil, http.StatusInternalServerError, appErr
	}

	if err := tx.Commit().Error; err != nil {
		log.WithError(err).Error("Failed to commit transaction")
		tx.Rollback()
		appErr := utils.NewError(nil, domain.ERR_FAILED_TO_CREATE_PROJECT_ERR_CODE,
			errors.New(domain.ERR_FAILED_TO_CREATE_PROJECT_ERR))
		return nil, http.StatusInternalServerError, appErr
	}

	log.WithField("id", project.ID).Info("Project created successfully")

	details := &domain.ProjectDetails{
		ID:          project.ID,
		Name:        project.Name,
		Description: project.Description,
	}
	return details, http.StatusOK, nil
}

func (s *ProjectService) DeleteProject(ctx context.Context, projectID uuid.UUID) (int, error) {
	log := cslog.L(ctx)

	log.WithField("id", projectID).Info("Deleting project")

	tx := s.db.Begin()
	if tx.Error != nil {
		log.WithError(tx.Error).Error("Failed to start transaction")
		appErr := utils.NewError(nil, domain.ERR_FAILED_TO_DELETE_PROJECT_ERR_CODE,
			errors.New(domain.ERR_FAILED_TO_DELETE_PROJECT_ERR))
		return http.StatusInternalServerError, appErr
	}

	project, err := s.repo.GetByID(ctx, tx, projectID)
	if err != nil {
		log.WithError(err).Error("Project not found")
		tx.Rollback()
		appErr := utils.NewError(nil, domain.ERR_FAILED_TO_DELETE_PROJECT_ERR_CODE,
			errors.New("Project not found"))
		return http.StatusNotFound, appErr
	}

	if err := s.repo.Delete(ctx, tx, project); err != nil {
		log.WithError(err).Error("Failed to delete project")
		tx.Rollback()
		appErr := utils.NewError(nil, domain.ERR_FAILED_TO_DELETE_PROJECT_ERR_CODE,
			errors.New(domain.ERR_FAILED_TO_DELETE_PROJECT_ERR))
		return http.StatusInternalServerError, appErr
	}

	secret, err := s.repo.GetSecret(ctx, tx, project.ID)
	if err != nil {
		log.WithError(err).Error("Failed to get project secret")
		tx.Rollback()
		appErr := utils.NewError(nil, domain.ERR_FAILED_TO_DELETE_PROJECT_ERR_CODE,
			errors.New(domain.ERR_FAILED_TO_DELETE_PROJECT_ERR))
		return http.StatusInternalServerError, appErr
	}

	if secret != nil {
		if err := s.repo.DeleteSecret(ctx, tx, secret); err != nil {
			log.WithError(err).Error("Failed to delete project secret")
			tx.Rollback()
			appErr := utils.NewError(nil, domain.ERR_FAILED_TO_DELETE_PROJECT_ERR_CODE,
				errors.New(domain.ERR_FAILED_TO_DELETE_PROJECT_ERR))
			return http.StatusInternalServerError, appErr
		}
	}

	if err := tx.Commit().Error; err != nil {
		log.WithError(err).Error("Failed to commit transaction")
		tx.Rollback()
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

	tmp, err := s.repo.GetByName(ctx, s.db, opts.Name)
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

	result, err := s.repo.List(ctx, s.db, opts)
	if err != nil {
		log.WithError(err).Error("Failed to list projects")
		appErr := utils.NewError(nil, domain.ERR_FAILED_TO_CREATE_PROJECT_ERR_CODE,
			errors.New("Failed to list projects"))
		return nil, http.StatusInternalServerError, appErr
	}

	return result, http.StatusOK, nil
}
