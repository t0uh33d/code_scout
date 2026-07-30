package services

import (
	"context"
	"time"

	"github.com/t0uh33d/code_scout/internal/ports"
	"github.com/t0uh33d/code_scout/pkg/cslog"
)

// RetentionService handles log cleanup: soft-deleting old logs and purging soft-deleted records.
type RetentionService struct {
	repo            ports.LogRepository
	retentionDays   int // logs older than this many days get soft-deleted
	purgeDaysAfter  int // soft-deleted logs older than this many days get permanently removed
}

func NewRetentionService(repo ports.LogRepository, retentionDays, purgeDaysAfter int) *RetentionService {
	if retentionDays <= 0 {
		retentionDays = 30
	}
	if purgeDaysAfter <= 0 {
		purgeDaysAfter = 7
	}
	return &RetentionService{
		repo:           repo,
		retentionDays:  retentionDays,
		purgeDaysAfter: purgeDaysAfter,
	}
}

// Cleanup runs the two-phase retention: soft-delete old logs, then purge old soft-deleted records.
// This is designed to be called by a cron job.
func (s *RetentionService) Cleanup(ctx context.Context) error {
	log := cslog.L(ctx)
	log.Info("Retention: starting cleanup cycle")

	// Phase 1: soft-delete logs past the retention window (all projects;
	// per-project windows can come with project-level retention settings)
	softDeleteBefore := time.Now().Add(-time.Duration(s.retentionDays) * 24 * time.Hour)
	deleted, err := s.repo.SoftDeleteBefore(ctx, softDeleteBefore)
	if err != nil {
		log.WithError(err).Error("Retention: soft-delete failed")
		return err
	}
	if deleted > 0 {
		log.WithField("count", deleted).Info("Retention: soft-deleted expired logs")
	}

	// Phase 2: permanently purge soft-deleted records past the grace period
	purgeOlderThan := time.Now().Add(-time.Duration(s.purgeDaysAfter) * 24 * time.Hour)
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
