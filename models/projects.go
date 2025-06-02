package models

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	error_codes "github.com/t0uh33d/code_scout/models/codes"
	"github.com/t0uh33d/code_scout/models/db"
	"github.com/t0uh33d/code_scout/utils"
	cslog "github.com/t0uh33d/code_scout/utils/cslog"
)

type projectCtrl struct {
	requestID    cslog.RequestID
	loggedInUser cslog.LoggedInUser
}

func NewProjectCtrl(requestCtrl cslog.RequestCtrl) *projectCtrl {
	ctrl := projectCtrl{}
	ctrl.requestID = requestCtrl.GetRequestID()
	ctrl.loggedInUser = requestCtrl.GetLoggedInUser()
	return &ctrl
}

func (pc *projectCtrl) GetRequestID() cslog.RequestID {
	return pc.requestID
}

func (pc *projectCtrl) GetLoggedInUser() cslog.LoggedInUser {
	return pc.loggedInUser
}

type CreateProjectOpts struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type ProjectDetails struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
}

func (pc *projectCtrl) CreateProject(opts *CreateProjectOpts) (*ProjectDetails, int, error) {
	log := cslog.NewRequestLog(pc)

	log.Info("Creating new project with name: ", opts.Name)
	if err := pc.validateCreateProjectOpts(opts, nil); err != nil {
		log.Error("Validation failed for creating project: ", err)
		err := utils.NewError(err, error_codes.ERR_FAILED_TO_CREATE_PROJECT_ERR_CODE,
			errors.New(error_codes.ERR_FAILED_TO_CREATE_PROJECT_ERR))
		return nil, http.StatusBadRequest, err
	}

	tx := db.GormDB.Begin()
	if tx.Error != nil {
		log.Error("Failed to start transaction: ", tx.Error)
		err := utils.NewError(nil, error_codes.ERR_FAILED_TO_CREATE_PROJECT_ERR_CODE,
			errors.New(error_codes.ERR_FAILED_TO_CREATE_PROJECT_ERR))
		return nil, http.StatusInternalServerError, err
	}

	project := &db.Projects{
		Name:        opts.Name,
		Description: opts.Description,
	}
	if err := project.Create(tx); err != nil {
		log.Error("Failed to create project: ", err)
		tx.Rollback()
		err := utils.NewError(nil, error_codes.ERR_FAILED_TO_CREATE_PROJECT_ERR_CODE,
			errors.New(error_codes.ERR_FAILED_TO_CREATE_PROJECT_ERR))
		return nil, http.StatusInternalServerError, err
	}

	secret := &db.ProjectSecret{
		ProjectID: project.ID.String(),
		SecretKey: utils.GenerateRandomString(32), // Generate a random secret key
	}
	if err := secret.Create(tx); err != nil {
		log.Error("Failed to create project secret: ", err)
		tx.Rollback()
		err := utils.NewError(nil, error_codes.ERR_FAILED_TO_CREATE_PROJECT_ERR_CODE,
			errors.New(error_codes.ERR_FAILED_TO_CREATE_PROJECT_ERR))
		return nil, http.StatusInternalServerError, err
	}

	if err := tx.Commit().Error; err != nil {
		log.Error("Failed to commit transaction: ", err)
		tx.Rollback()
		err := utils.NewError(nil, error_codes.ERR_FAILED_TO_CREATE_PROJECT_ERR_CODE,
			errors.New(error_codes.ERR_FAILED_TO_CREATE_PROJECT_ERR))
		return nil, http.StatusInternalServerError, err
	}

	log.Info("Project created successfully with ID: ", project.ID)

	return serializeProjectDetails(project), http.StatusOK, nil
}

func (pc *projectCtrl) DeleteProject(projectID uuid.UUID) (int, error) {
	log := cslog.NewRequestLog(pc)

	log.Info("Deleting project with ID: ", projectID)

	tx := db.GormDB.Begin()
	if tx.Error != nil {
		log.Error("Failed to start transaction: ", tx.Error)
		err := utils.NewError(nil, error_codes.ERR_FAILED_TO_DELETE_PROJECT_ERR_CODE,
			errors.New(error_codes.ERR_FAILED_TO_DELETE_PROJECT_ERR))
		return http.StatusInternalServerError, err
	}

	project, err := db.GetProjectByID(tx, projectID)
	if err != nil {
		log.Error("Project not found: ", err)
		tx.Rollback()
		err := utils.NewError(nil, error_codes.ERR_FAILED_TO_DELETE_PROJECT_ERR_CODE,
			errors.New("Project not found"))
		return http.StatusNotFound, err
	}

	if err := project.Delete(tx); err != nil {
		log.Error("Failed to delete project: ", err)
		tx.Rollback()
		err := utils.NewError(nil, error_codes.ERR_FAILED_TO_DELETE_PROJECT_ERR_CODE,
			errors.New(error_codes.ERR_FAILED_TO_DELETE_PROJECT_ERR))
		return http.StatusInternalServerError, err
	}

	// GetProjectSecretByProjectID
	secret, err := db.GetProjectSecretByProjectID(tx, project.ID)
	if err != nil {
		log.Error("Failed to get project secret: ", err)
		tx.Rollback()
		err := utils.NewError(nil, error_codes.ERR_FAILED_TO_DELETE_PROJECT_ERR_CODE,
			errors.New(error_codes.ERR_FAILED_TO_DELETE_PROJECT_ERR))
		return http.StatusInternalServerError, err
	}

	if secret != nil {
		if err := secret.Delete(tx); err != nil {
			log.Error("Failed to delete project secret: ", err)
			tx.Rollback()
			err := utils.NewError(nil, error_codes.ERR_FAILED_TO_DELETE_PROJECT_ERR_CODE,
				errors.New(error_codes.ERR_FAILED_TO_DELETE_PROJECT_ERR))
			return http.StatusInternalServerError, err
		}
	}

	if err := tx.Commit().Error; err != nil {
		log.Error("Failed to commit transaction: ", err)
		tx.Rollback()
		err := utils.NewError(nil, error_codes.ERR_FAILED_TO_DELETE_PROJECT_ERR_CODE,
			errors.New(error_codes.ERR_FAILED_TO_DELETE_PROJECT_ERR))
		return http.StatusInternalServerError, err
	}

	log.Info("Project deleted successfully with ID: ", projectID)

	return http.StatusOK, nil
}

func serializeProjectDetails(project *db.Projects) *ProjectDetails {
	return &ProjectDetails{
		ID:          project.ID,
		Name:        project.Name,
		Description: project.Description,
	}
}

func (gn *projectCtrl) validateCreateProjectOpts(opts *CreateProjectOpts, id *uuid.UUID) []utils.FieldError {
	attrErrs := []utils.FieldError{}

	if opts.Name == "" {
		err := utils.CreateFieldError(error_codes.ERR_INVALID_PROJECT_NAME_ERR_CODE,
			error_codes.ERR_INVALID_PROJECT_NAME_ERR, "name", "Project name cannot empty")
		attrErrs = append(attrErrs, err)
	}

	if len(attrErrs) > 0 {
		return attrErrs
	}

	tmp, err := db.GetProjectByName(db.GormDB, opts.Name)
	if err == nil {
		if id != nil && tmp.ID == *id {
			return nil
		}

		err := utils.CreateFieldError(error_codes.ERR_INVALID_PROJECT_NAME_ERR_CODE,
			error_codes.ERR_INVALID_PROJECT_NAME_ERR, "name", "Project with a same name already exists")
		return []utils.FieldError{err}
	}

	return nil
}
