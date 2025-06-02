package ctrls

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/t0uh33d/code_scout/middleware"
	"github.com/t0uh33d/code_scout/models"
	error_codes "github.com/t0uh33d/code_scout/models/codes"
	"github.com/t0uh33d/code_scout/utils"
	"github.com/t0uh33d/code_scout/utils/cslog"
)

func CreateProject(w http.ResponseWriter, r *http.Request) {
	reqCtrl := cslog.NewRequestCtrl(r)
	log := cslog.NewRequestLog(reqCtrl)

	opts := &models.CreateProjectOpts{}
	err := json.NewDecoder(r.Body).Decode(opts)
	if err != nil {
		log.Error(err)
		err = utils.NewError(
			nil, error_codes.INVALID_REQUEST_DATA_ERR_CODE,
			errors.New(error_codes.INVALID_REQUEST_DATA_ERR),
		)
		utils.HttpError(w, http.StatusBadRequest, err)
		return
	}

	projectCtrl := models.NewProjectCtrl(reqCtrl)
	resp, status, err := projectCtrl.CreateProject(opts)
	if err != nil {
		log.Error(err)
		utils.HttpError(w, status, err)
		return
	}

	b, _ := json.Marshal(resp)

	w.WriteHeader(status)
	w.Write(b)
}

func DeleteProject(w http.ResponseWriter, r *http.Request) {
	reqCtrl := cslog.NewRequestCtrl(r)
	log := cslog.NewRequestLog(reqCtrl)

	projectID, err := utils.GetUUIDParam(r, "project_id")
	if err != nil {
		log.Error(err)
		err = utils.NewError(
			nil, error_codes.INVALID_REQUEST_DATA_ERR_CODE,
			errors.New(error_codes.INVALID_REQUEST_DATA_ERR),
		)
		utils.HttpError(w, http.StatusBadRequest, err)
		return
	}

	projectCtrl := models.NewProjectCtrl(reqCtrl)
	status, err := projectCtrl.DeleteProject(projectID)
	if err != nil {
		log.Error(err)
		utils.HttpError(w, status, err)
		return
	}

	w.WriteHeader(status)
}

func Validate(w http.ResponseWriter, r *http.Request) {
	reqCtrl := cslog.NewRequestCtrl(r)
	log := cslog.NewRequestLog(reqCtrl)

	project, err := middleware.GetProjectFromContext(r.Context())
	if err != nil {
		log.Error(err)
		utils.HttpError(w, http.StatusBadRequest, err)
		return
	}

	b, _ := json.Marshal(project)

	w.WriteHeader(http.StatusOK)
	w.Write(b)
}
