package jobs

import (
	"context"

	"github.com/robfig/cron/v3"
	"github.com/t0uh33d/code_scout/internal/services"
	"github.com/t0uh33d/code_scout/pkg/cslog"
)

func StartScheduler(ctx context.Context, retentionSvc *services.RetentionService) {
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

	c.Start()
}
