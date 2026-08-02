package services

import (
	"context"
	"time"

	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/internal/ports"
	"github.com/t0uh33d/code_scout/pkg/cslog"
)

// Orphan reaping is batched so a single huge project cannot hold the cleanup
// cycle open. At these numbers a run clears up to a million rows and anything
// beyond that waits for tomorrow.
const (
	orphanBatchSize  = 5000
	orphanMaxBatches = 200
)

// RetentionService handles log cleanup: soft-deleting old logs and purging soft-deleted records.
//
// The windows are read from instance settings at the top of every run, not
// captured at construction. The cron holds this service for the life of the
// process, so a value taken once would mean an operator's change did not apply
// until the next restart — and they would have no way to tell.
type RetentionService struct {
	repo     ports.LogRepository
	settings *InstanceSettingsService
}

func NewRetentionService(repo ports.LogRepository, settings *InstanceSettingsService) *RetentionService {
	return &RetentionService{repo: repo, settings: settings}
}

// Cleanup runs the two-phase retention: soft-delete old logs, then purge old soft-deleted records.
// This is designed to be called by a cron job.
func (s *RetentionService) Cleanup(ctx context.Context) error {
	log := cslog.L(ctx)

	// Settings that were never read are settings we do not know. Everything
	// below deletes rows, so a boot where the settings row was unreadable must
	// skip the run rather than delete against defaults — an operator keeping a
	// year of logs would lose eleven months of them to a transient DB blip.
	if !s.settings.Loaded() {
		log.Warn("Retention: instance settings were never loaded, skipping this run")
		return nil
	}

	settings := s.settings.Current()
	retentionDays := settings.RetentionDays
	purgeDaysAfter := settings.PurgeAfterDays

	// Defence in depth behind the form's validation. Retention of 0 puts the
	// cutoff at "now" and soft-deletes every log in the instance, so a value
	// that somehow got past validation is refused here too rather than obeyed.
	if !domain.ValidRetentionDays(retentionDays) || !domain.ValidPurgeAfterDays(purgeDaysAfter) {
		log.WithField("retention_days", retentionDays).
			WithField("purge_after_days", purgeDaysAfter).
			Error("Retention: refusing to run against out-of-range settings")
		return nil
	}

	log.WithField("retention_days", retentionDays).
		WithField("purge_after_days", purgeDaysAfter).
		Info("Retention: starting cleanup cycle")

	// Phase 0: reap logs whose project was deleted. Deleting a project is O(1)
	// by design, so this is where its logs actually go. Bounded per run so one
	// enormous project cannot monopolise the cycle; whatever is left is picked
	// up tomorrow.
	orphaned := 0
	for range orphanMaxBatches {
		n, err := s.repo.PurgeOrphanedLogs(ctx, orphanBatchSize)
		if err != nil {
			log.WithError(err).Error("Retention: orphan purge failed")
			break
		}
		orphaned += int(n)
		if int(n) < orphanBatchSize {
			break
		}
	}
	if orphaned > 0 {
		log.WithField("count", orphaned).Info("Retention: purged logs from deleted projects")
	}

	// Phase 1: soft-delete logs past the retention window (all projects;
	// per-project windows can come with project-level retention settings)
	softDeleteBefore := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	deleted, err := s.repo.SoftDeleteBefore(ctx, softDeleteBefore)
	if err != nil {
		log.WithError(err).Error("Retention: soft-delete failed")
		return err
	}
	if deleted > 0 {
		log.WithField("count", deleted).Info("Retention: soft-deleted expired logs")
	}

	// Phase 2: permanently purge soft-deleted records past the grace period
	purgeOlderThan := time.Now().Add(-time.Duration(purgeDaysAfter) * 24 * time.Hour)
	purged, err := s.repo.PurgeSoftDeleted(ctx, purgeOlderThan)
	if err != nil {
		log.WithError(err).Error("Retention: purge failed")
		return err
	}
	if purged > 0 {
		log.WithField("count", purged).Info("Retention: purged soft-deleted logs")
	}

	log.Info("Retention: cleanup cycle complete")
	return nil
}
