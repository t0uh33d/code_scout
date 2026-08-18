package jobs

import (
	"context"

	"github.com/robfig/cron/v3"
	"github.com/getcodescout/code_scout/internal/services"
	"github.com/getcodescout/code_scout/pkg/cslog"
)

// StartScheduler runs the instance's recurring work.
//
// checkVersion is passed in rather than a service being imported, so this
// package stays free of the view layer the result has to reach. Wiring belongs
// in main.go, which is where the closure is built.
func StartScheduler(ctx context.Context, retentionSvc *services.RetentionService, checkVersion func(context.Context)) {
	log := cslog.L(ctx)
	log.Info("Initialize CodeScout Service Scheduler...")

	c := cron.New()

	// Run retention cleanup daily at 3:00 AM
	c.AddFunc("0 3 * * *", func() {
		log.Info("Cron: running retention cleanup")
		if err := retentionSvc.Cleanup(ctx); err != nil {
			log.WithError(err).Error("Cron: retention cleanup failed")
		}
	})

	// Once a day, at a minute nobody else is using. Daily rather than hourly
	// because releases are not frequent and the answer is not urgent: nothing
	// breaks by learning about an upgrade eight hours late.
	//
	// 04:17 rather than the top of an hour so that every CodeScout instance in
	// the world does not ask GitHub the same question at the same second.
	if checkVersion != nil {
		c.AddFunc("17 4 * * *", func() {
			checkVersion(ctx)
		})
	}

	c.Start()
}
