package services

import (
	"context"
	"time"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/ports"
	"github.com/getcodescout/code_scout/pkg/cslog"
)

// Orphan reaping is batched so a single huge project cannot hold the cleanup
// cycle open. At these numbers a run clears up to a million rows and anything
// beyond that waits for tomorrow.
const (
	orphanBatchSize  = 5000
	orphanMaxBatches = 200

	// usageGraceDays keeps a day's counter around after its logs have gone, so
	// "how much did this project send last month" survives the logs themselves.
	usageGraceDays = 90
)

// RetentionService handles log cleanup: soft-deleting old logs and purging soft-deleted records.
//
// The windows are read from instance settings at the top of every run, not
// captured at construction. The cron holds this service for the life of the
// process, so a value taken once would mean an operator's change did not apply
// until the next restart — and they would have no way to tell.
type RetentionService struct {
	repo     ports.LogRepository
	usage    ports.UsageRepository
	sessions ports.SessionRepository
	settings *InstanceSettingsService
}

func NewRetentionService(repo ports.LogRepository, usage ports.UsageRepository, sessions ports.SessionRepository, settings *InstanceSettingsService) *RetentionService {
	return &RetentionService{repo: repo, usage: usage, sessions: sessions, settings: settings}
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

	// Phase 3: the daily usage counters. One small row per project per day, but
	// nothing else would ever delete from them, so they would grow forever.
	// Kept a good while past the retention window: they are the only record of
	// what a project sent on a day whose logs have already gone.
	if s.usage != nil {
		usageBefore := time.Now().Add(-time.Duration(retentionDays+usageGraceDays) * 24 * time.Hour)
		if counters, err := s.usage.PurgeUsageBefore(ctx, usageBefore); err != nil {
			log.WithError(err).Error("Retention: usage counter purge failed")
		} else if counters > 0 {
			log.WithField("count", counters).Info("Retention: purged old usage counters")
		}
	}

	// Phase 4: sessions. One row per app launch per device, and until this
	// existed nothing deleted from the table at all — not retention, not
	// deleting the project. An app with real users writes them faster than
	// anything else in the schema, so it was the one table that grew without
	// bound whatever an operator set retention to.
	//
	// Cut at the purge horizon rather than the retention window, because a
	// session is what a log's session_id resolves through. Removing it while
	// its logs are only soft-deleted would strip those logs out of every
	// session-scoped search (`user:`, `device:`, `app_version:`) without
	// deleting them, which reads as data loss and is worse than the growth.
	if s.sessions != nil {
		orphanedSessions := 0
		for range orphanMaxBatches {
			n, err := s.sessions.PurgeOrphanedSessions(ctx, orphanBatchSize)
			if err != nil {
				log.WithError(err).Error("Retention: orphaned session purge failed")
				break
			}
			orphanedSessions += int(n)
			if int(n) < orphanBatchSize {
				break
			}
		}
		if orphanedSessions > 0 {
			log.WithField("count", orphanedSessions).
				Info("Retention: purged sessions from deleted projects")
		}

		// retention + purge, not purgeOlderThan. A log is soft-deleted at the
		// retention window and only removed for good purgeDaysAfter later, so
		// the horizon where its row has actually gone is the sum. purgeOlderThan
		// on its own is the *shorter* of the two windows and would have deleted
		// sessions three weeks before the logs pointing at them.
		sessionsBefore := time.Now().Add(-time.Duration(retentionDays+purgeDaysAfter) * 24 * time.Hour)
		if sessions, err := s.sessions.PurgeSessionsBefore(ctx, sessionsBefore); err != nil {
			log.WithError(err).Error("Retention: session purge failed")
		} else if sessions > 0 {
			log.WithField("count", sessions).Info("Retention: purged expired sessions")
		}
	}

	log.Info("Retention: cleanup cycle complete")
	return nil
}
