package jobs

import (
	"context"

	"github.com/robfig/cron/v3"
	"github.com/t0uh33d/code_scout/pkg/cslog"
)

func StartScheduler(ctx context.Context) {
	log := cslog.L(ctx)
	log.Info("Initialize CodeScout Service Scheduler...")

	c := cron.New()

	c.Start()
}
