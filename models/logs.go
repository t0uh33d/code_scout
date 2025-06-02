package models

import (
	"archive/tar"

	"github.com/t0uh33d/code_scout/models/db"
	cslog "github.com/t0uh33d/code_scout/utils/cslog"
)

type logCtrl struct {
	requestID    cslog.RequestID
	loggedInUser cslog.LoggedInUser
}

func NewLogCtrl(requestCtrl cslog.RequestCtrl) *logCtrl {
	ctrl := logCtrl{}
	ctrl.requestID = requestCtrl.GetRequestID()
	ctrl.loggedInUser = requestCtrl.GetLoggedInUser()
	return &ctrl
}

func (lc *logCtrl) GetRequestID() cslog.RequestID {
	return lc.requestID
}

func (lc *logCtrl) GetLoggedInUser() cslog.LoggedInUser {
	return lc.loggedInUser
}

func (lc *logCtrl) DumpLogs(project *db.Projects, tr *tar.Reader) (int, error) {
	log := cslog.NewRequestLog(lc)
	log.Info("Dumping logs...")

	return 200, nil
}
