package ctrls

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"net/http"

	"github.com/t0uh33d/code_scout/middleware"
	"github.com/t0uh33d/code_scout/models"
	error_codes "github.com/t0uh33d/code_scout/models/codes"
	"github.com/t0uh33d/code_scout/utils"
	"github.com/t0uh33d/code_scout/utils/cslog"
)

func DumpLogs(w http.ResponseWriter, r *http.Request) {
	reqCtrl := cslog.NewRequestCtrl(r)
	log := cslog.NewRequestLog(reqCtrl)

	project, err := middleware.GetProjectFromContext(r.Context())
	if err != nil {
		log.Error(err)
		utils.HttpError(w, http.StatusBadRequest, err)
		return
	}

	err = r.ParseMultipartForm(10 << 20)
	if err != nil {
		log.Error(err)
		err = utils.NewError(nil, error_codes.INVALID_REQUEST_DATA_ERR_CODE,
			errors.New("failed to parse multipart form data"))
		utils.HttpError(w, http.StatusBadRequest, err)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		log.Error(err)
		err = utils.NewError(nil, error_codes.INVALID_REQUEST_DATA_ERR_CODE,
			errors.New("failed to get file from form data"))
		utils.HttpError(w, http.StatusBadRequest, err)
		return
	}

	defer file.Close()

	gzr, err := gzip.NewReader(file)
	if err != nil {
		log.Error(err)
		err = utils.NewError(nil, error_codes.INVALID_REQUEST_DATA_ERR_CODE,
			errors.New("failed to create gzip reader"))
		utils.HttpError(w, http.StatusBadRequest, err)
		return
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	logCtrl := models.NewLogCtrl(reqCtrl)
	status, err := logCtrl.DumpLogs(project, tr)
	if err != nil {
		log.Error(err)
		utils.HttpError(w, status, err)
		return
	}

	w.WriteHeader(status)
}
