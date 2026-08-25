package services

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/ports"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"github.com/getcodescout/code_scout/pkg/utils"
	"github.com/google/uuid"
)

type ProjectService struct {
	repo    ports.ProjectRepository
	members ports.MemberRepository
	txMgr   ports.TransactionManager
}

func NewProjectService(repo ports.ProjectRepository, members ports.MemberRepository, txMgr ports.TransactionManager) *ProjectService {
	return &ProjectService{
		repo:    repo,
		members: members,
		txMgr:   txMgr,
	}
}

func (s *ProjectService) CreateProject(ctx context.Context, opts *domain.CreateProjectOpts) (*domain.ProjectDetails, int, error) {
	log := cslog.L(ctx)

	opts.Name = strings.TrimSpace(opts.Name)
	opts.Description = strings.TrimSpace(opts.Description)

	log.WithField("name", opts.Name).Info("Creating new project")
	if fieldErrs := s.validateProjectName(ctx, opts.Name, nil); len(fieldErrs) > 0 {
		log.Error("Validation failed for creating project")
		appErr := utils.NewError(fieldErrs, domain.ERR_INVALID_PROJECT_NAME_ERR_CODE,
			errors.New(domain.ERR_INVALID_PROJECT_NAME_ERR))
		return nil, http.StatusBadRequest, appErr
	}

	project := &domain.Project{
		Name:              opts.Name,
		Description:       opts.Description,
		SessionSampleRate: domain.FullSampleRate,
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

		// The creator becomes a maintainer in the same transaction. Visibility
		// comes from membership, so without this an admin would create a
		// project and instantly lose sight of it.
		if opts.CreatedBy != uuid.Nil {
			if err := s.members.SetMembership(txCtx, opts.CreatedBy, project.ID, domain.LevelMaintainer); err != nil {
				return err
			}
		}
		for _, m := range opts.Members {
			if m.UserID == uuid.Nil || m.UserID == opts.CreatedBy || !m.Level.Valid() {
				continue
			}
			if err := s.members.SetMembership(txCtx, m.UserID, project.ID, m.Level); err != nil {
				return err
			}
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

	// Logs are deliberately not deleted here. A project can hold millions of
	// rows and the server's write timeout is 30s, so an inline delete would
	// time out, roll the transaction back, and leave the project undeleted.
	// The nightly retention job reaps logs whose project is gone.
	err := s.txMgr.WithTransaction(ctx, func(txCtx context.Context) error {
		project, err := s.repo.LockProject(txCtx, projectID)
		if err != nil {
			return err
		}
		// Deleting by project, not by row: a project with no secret is a
		// no-op rather than an error. Reading the secret first is what used to
		// abort the whole transaction and leave the project undeleted.
		if _, err := s.repo.DeleteSecretsByProject(txCtx, projectID); err != nil {
			return err
		}
		if _, err := s.repo.DeleteFavoritesByProject(txCtx, projectID); err != nil {
			return err
		}
		// Memberships go too, or a deleted project keeps granting access to
		// whatever id reuse or a restore might produce.
		if err := s.members.RemoveAllForProject(txCtx, projectID); err != nil {
			return err
		}
		return s.repo.Delete(txCtx, project)
	})
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return http.StatusNotFound, utils.NewError(nil, domain.ERR_PROJECT_NOT_FOUND_ERR_CODE,
				errors.New(domain.ERR_PROJECT_NOT_FOUND_ERR))
		}
		log.WithError(err).Error("Failed to delete project")
		appErr := utils.NewError(nil, domain.ERR_FAILED_TO_DELETE_PROJECT_ERR_CODE,
			errors.New(domain.ERR_FAILED_TO_DELETE_PROJECT_ERR))
		return http.StatusInternalServerError, appErr
	}

	log.WithField("id", projectID).Info("Project deleted successfully")
	return http.StatusOK, nil
}

// validateProjectName checks a name is present and unused. excludeID lets a
// rename keep its own name: without it, saving an unchanged name would collide
// with the project itself.
func (s *ProjectService) validateProjectName(ctx context.Context, name string, excludeID *uuid.UUID) []utils.FieldError {
	nameErr := func(detail string) []utils.FieldError {
		return []utils.FieldError{utils.CreateFieldError(
			domain.ERR_INVALID_PROJECT_NAME_ERR_CODE,
			domain.ERR_INVALID_PROJECT_NAME_ERR, "name", detail)}
	}

	if name == "" {
		return nameErr("Project name cannot be empty")
	}

	existing, err := s.repo.GetByName(ctx, name)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		// A failed read is not proof the name is free. Refusing on a transient
		// error beats admitting a duplicate.
		return nameErr("Could not check that name. Try again.")
	}
	if excludeID != nil && existing.ID == *excludeID {
		return nil
	}
	return nameErr("A project with that name already exists")
}

// GetProject loads a project for display.
func (s *ProjectService) GetProject(ctx context.Context, projectID uuid.UUID) (*domain.Project, int, error) {
	project, err := s.repo.GetByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, http.StatusNotFound, utils.NewError(nil, domain.ERR_PROJECT_NOT_FOUND_ERR_CODE,
				errors.New(domain.ERR_PROJECT_NOT_FOUND_ERR))
		}
		return nil, http.StatusInternalServerError, utils.NewError(nil, domain.ERR_PROJECT_NOT_FOUND_ERR_CODE,
			errors.New(domain.ERR_PROJECT_NOT_FOUND_ERR))
	}
	return project, http.StatusOK, nil
}

// UpdateProject renames a project and edits its description. Field errors are
// returned as a *utils.ErrorJson carrying Errors, so the caller can render them
// against the offending input rather than as a banner.
func (s *ProjectService) UpdateProject(ctx context.Context, projectID uuid.UUID, opts *domain.UpdateProjectOpts) (*domain.Project, int, error) {
	log := cslog.L(ctx)

	opts.Name = strings.TrimSpace(opts.Name)
	opts.Description = strings.TrimSpace(opts.Description)

	var updated *domain.Project
	err := s.txMgr.WithTransaction(ctx, func(txCtx context.Context) error {
		// Lock first: the uniqueness check and the write must not straddle a
		// concurrent rename of this same project.
		project, err := s.repo.LockProject(txCtx, projectID)
		if err != nil {
			return err
		}

		fieldErrs := s.validateProjectName(txCtx, opts.Name, &projectID)
		if opts.SessionSampleRate != nil && !domain.ValidSampleRate(*opts.SessionSampleRate) {
			fieldErrs = append(fieldErrs, utils.FieldError{
				Field:  "session_sample_rate",
				Detail: "Sampling must be between 0 and 100 percent.",
			})
		}
		if len(fieldErrs) > 0 {
			return utils.NewError(fieldErrs, domain.ERR_INVALID_PROJECT_NAME_ERR_CODE,
				errors.New(domain.ERR_INVALID_PROJECT_NAME_ERR))
		}

		project.Name = opts.Name
		project.Description = opts.Description
		if opts.SessionSampleRate != nil {
			project.SessionSampleRate = *opts.SessionSampleRate
		}
		if err := s.repo.Update(txCtx, project); err != nil {
			return err
		}
		updated = project
		return nil
	})
	if err != nil {
		var appErr *utils.ErrorJson
		if errors.As(err, &appErr) {
			return nil, http.StatusBadRequest, appErr
		}
		if errors.Is(err, domain.ErrNotFound) {
			return nil, http.StatusNotFound, utils.NewError(nil, domain.ERR_PROJECT_NOT_FOUND_ERR_CODE,
				errors.New(domain.ERR_PROJECT_NOT_FOUND_ERR))
		}
		log.WithError(err).Error("Failed to update project")
		return nil, http.StatusInternalServerError, utils.NewError(nil,
			domain.ERR_FAILED_TO_UPDATE_PROJECT_ERR_CODE, errors.New(domain.ERR_FAILED_TO_UPDATE_PROJECT_ERR))
	}
	return updated, http.StatusOK, nil
}

// RevealSecret returns the plaintext secret. This is the only call that hands
// out a live credential, so it is logged.
func (s *ProjectService) RevealSecret(ctx context.Context, projectID uuid.UUID) (string, int, error) {
	secret, err := s.repo.GetSecret(ctx, projectID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", http.StatusNotFound, utils.NewError(nil, domain.ERR_PROJECT_SECRET_MISSING_ERR_CODE,
				errors.New(domain.ERR_PROJECT_SECRET_MISSING_ERR))
		}
		return "", http.StatusInternalServerError, utils.NewError(nil, domain.ERR_PROJECT_SECRET_MISSING_ERR_CODE,
			errors.New(domain.ERR_PROJECT_SECRET_MISSING_ERR))
	}

	cslog.L(ctx).WithField("project_id", projectID).Warn("Project secret revealed")
	return secret.SecretKey, http.StatusOK, nil
}

// RotateSecret mints a new secret and invalidates the old one, returning the
// new plaintext once.
func (s *ProjectService) RotateSecret(ctx context.Context, projectID uuid.UUID) (string, int, error) {
	log := cslog.L(ctx)

	newKey := utils.GenerateRandomString(32)
	err := s.txMgr.WithTransaction(ctx, func(txCtx context.Context) error {
		// The lock is what guarantees one live secret. Without it two
		// concurrent rotations each delete what they can see and each insert,
		// leaving two rows, and GetSecret would keep returning the older one.
		if _, err := s.repo.LockProject(txCtx, projectID); err != nil {
			return err
		}
		_, err := s.repo.ReplaceSecret(txCtx, projectID, newKey)
		return err
	})
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", http.StatusNotFound, utils.NewError(nil, domain.ERR_PROJECT_NOT_FOUND_ERR_CODE,
				errors.New(domain.ERR_PROJECT_NOT_FOUND_ERR))
		}
		log.WithError(err).Error("Failed to rotate project secret")
		return "", http.StatusInternalServerError, utils.NewError(nil,
			domain.ERR_FAILED_TO_ROTATE_SECRET_ERR_CODE, errors.New(domain.ERR_FAILED_TO_ROTATE_SECRET_ERR))
	}

	log.WithField("project_id", projectID).Warn("Project secret rotated")
	return newKey, http.StatusOK, nil
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

// ToggleFavorite flips the star and returns the new state, so the caller can
// re-render the control without asking again.
func (s *ProjectService) ToggleFavorite(ctx context.Context, userID, projectID uuid.UUID) (bool, int, error) {
	log := cslog.L(ctx)

	// project_favorites carries no FK (soft-deleted projects keep their row),
	// so a fabricated id would otherwise store an orphan and render a lit star
	// for a project that does not exist.
	if _, err := s.repo.GetByID(ctx, projectID); err != nil {
		return false, http.StatusNotFound, utils.NewError(nil, domain.INVALID_PROJECT_ID_HEADER_ERR_CODE,
			errors.New("Project not found"))
	}

	current, err := s.repo.IsFavorite(ctx, userID, projectID)
	if err != nil {
		log.WithError(err).Error("Failed to read favorite state")
		return false, http.StatusInternalServerError, utils.NewError(nil, domain.ERR_FAILED_TO_CREATE_PROJECT_ERR_CODE,
			errors.New("Failed to update favorite"))
	}

	if err := s.repo.SetFavorite(ctx, userID, projectID, !current); err != nil {
		log.WithError(err).Error("Failed to set favorite")
		return false, http.StatusInternalServerError, utils.NewError(nil, domain.ERR_FAILED_TO_CREATE_PROJECT_ERR_CODE,
			errors.New("Failed to update favorite"))
	}

	return !current, http.StatusOK, nil
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
