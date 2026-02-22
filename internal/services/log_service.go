package services

import (
	"archive/tar"
	"context"
	"encoding/json"

	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/internal/ports"
	"github.com/t0uh33d/code_scout/pkg/cslog"
	"gorm.io/gorm"
)

type LogService struct {
	repo ports.LogRepository
	db   *gorm.DB
}

func NewLogService(repo ports.LogRepository, db *gorm.DB) *LogService {
	return &LogService{
		repo: repo,
		db:   db,
	}
}

func (s *LogService) DumpLogs(ctx context.Context, project *domain.Project, tr *tar.Reader) (int, error) {
	log := cslog.L(ctx)
	log.Info("Dumping logs...")

	var allLogs []domain.IncomingLog
	for {
		header, err := tr.Next()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			log.WithError(err).Error("Failed to read tar entry")
			return 500, err
		}

		if header.Typeflag != tar.TypeReg {
			log.WithField("file", header.Name).Warn("Skipping non-regular file")
			continue
		}

		var rawLogs []domain.IncomingLog
		if err := json.NewDecoder(tr).Decode(&rawLogs); err != nil {
			log.WithError(err).Error("Failed to decode log file")
			return 500, err
		}

		allLogs = append(allLogs, rawLogs...)
	}

	if err := s.insertIncomingLogs(ctx, allLogs); err != nil {
		log.WithError(err).Error("Failed to insert incoming logs")
		return 500, err
	}

	return 200, nil
}

func (s *LogService) insertIncomingLogs(ctx context.Context, logs []domain.IncomingLog) error {
	log := cslog.L(ctx)
	log.WithField("count", len(logs)).Info("Inserting incoming logs...")

	tx := s.db.Begin()
	if tx.Error != nil {
		log.WithError(tx.Error).Error("Failed to start transaction")
		return tx.Error
	}

	var domainLogs []domain.Log
	for _, logEntry := range logs {
		domainLog := domain.Log{
			SessionID:     logEntry.SessionID,
			Level:         logEntry.Level,
			Message:       logEntry.Message,
			Error:         logEntry.Error,
			StackTrace:    logEntry.StackTrace,
			Metadata:      logEntry.Metadata,
			Tags:          logEntry.Tags,
			TimeStamp:     logEntry.Timestamp,
			IsNetworkCall: bool(logEntry.IsNetworkCall),
			RequestID:     logEntry.RequestID,
			CallPhase:     logEntry.CallPhase,
		}
		domainLogs = append(domainLogs, domainLog)
	}

	if err := s.repo.CreateBatch(ctx, tx, domainLogs); err != nil {
		log.WithError(err).Error("Failed to create batch logs")
		tx.Rollback()
		return err
	}

	if err := tx.Commit().Error; err != nil {
		log.WithError(err).Error("Failed to commit transaction")
		tx.Rollback()
		return err
	}

	return nil
}
