package jobs

import (
	"github.com/robfig/cron/v3"
	"github.com/t0uh33d/code_scout/utils/cslog"
)

type schedulerCtrls struct {
	requestID    cslog.RequestID
	loggedInUser cslog.LoggedInUser
}

func NewSchedulerCtrls(requestCtrl cslog.RequestCtrl) *schedulerCtrls {
	ctrl := schedulerCtrls{}
	ctrl.requestID = requestCtrl.GetRequestID()
	ctrl.loggedInUser = requestCtrl.GetLoggedInUser()
	return &ctrl
}

func (dc *schedulerCtrls) GetRequestID() cslog.RequestID {
	return dc.requestID
}

func (dc *schedulerCtrls) GetLoggedInUser() cslog.LoggedInUser {
	return dc.loggedInUser
}

func (sc *schedulerCtrls) Scheduler() {
	reqCtrl := cslog.NewRequestLog(cslog.RequestLog{
		RequestID:    sc.GetRequestID(),
		LoggedInUser: sc.GetLoggedInUser(),
	})
	log := cslog.NewRequestLog(reqCtrl)

	log.Info("Initialize CodeScout Service Scheduler...")

	c := cron.New()

	// c.AddFunc("@every "+"5m", func() {
	// 	reqCtrl := oalog.RequestLog{
	// 		RequestID:    oalog.RequestID("cron-job-" + uuid.NewString()),
	// 		LoggedInUser: sc.GetLoggedInUser(),
	// 	}
	// 	syncCtrl := oalog.NewRequestLog(reqCtrl)
	// 	scSyncS := NewSchedulerCtrls(syncCtrl)
	// 	log := oalog.NewRequestLog(scSyncS)
	// 	log.Info("Cron job to update the status (every " + "5s)")
	// })

	c.Start()
}
