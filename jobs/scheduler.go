package jobs

import (
	"github.com/robfig/cron/v3"
	"github.com/t0uh33d/code_scout/utils/oalog"
)

type schedulerCtrls struct {
	requestID    oalog.RequestID
	loggedInUser oalog.LoggedInUser
}

func NewSchedulerCtrls(requestCtrl oalog.RequestCtrl) *schedulerCtrls {
	ctrl := schedulerCtrls{}
	ctrl.requestID = requestCtrl.GetRequestID()
	ctrl.loggedInUser = requestCtrl.GetLoggedInUser()
	return &ctrl
}

func (dc *schedulerCtrls) GetRequestID() oalog.RequestID {
	return dc.requestID
}

func (dc *schedulerCtrls) GetLoggedInUser() oalog.LoggedInUser {
	return dc.loggedInUser
}

func (sc *schedulerCtrls) Scheduler() {
	reqCtrl := oalog.NewRequestLog(oalog.RequestLog{
		RequestID:    sc.GetRequestID(),
		LoggedInUser: sc.GetLoggedInUser(),
	})
	log := oalog.NewRequestLog(reqCtrl)

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
