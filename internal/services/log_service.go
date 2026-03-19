package services

import (
	"archive/tar"
	"context"
	"encoding/json"

	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/internal/ports"
	"github.com/t0uh33d/code_scout/pkg/cslog"
)

type LogService struct {
	repo      ports.LogRepository
	txMgr     ports.TransactionManager
	publisher ports.EventPublisher // nil-safe: if nil, no SSE events are published
}

func NewLogService(repo ports.LogRepository, txMgr ports.TransactionManager, publisher ports.EventPublisher) *LogService {
	return &LogService{
		repo:      repo,
		txMgr:     txMgr,
		publisher: publisher,
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

	domainLogs, err := s.insertIncomingLogs(ctx, project, allLogs)
	if err != nil {
		log.WithError(err).Error("Failed to insert incoming logs")
		return 500, err
	}

	// Publish to SSE broker for real-time streaming (nil-safe)
	if s.publisher != nil && len(domainLogs) > 0 {
		s.publisher.Publish(project.ID, domainLogs)
	}

	return 200, nil
}

func (s *LogService) insertIncomingLogs(ctx context.Context, project *domain.Project, logs []domain.IncomingLog) ([]domain.Log, error) {
	log := cslog.L(ctx)
	log.WithField("count", len(logs)).Info("Inserting incoming logs...")

	var domainLogs []domain.Log
	for _, logEntry := range logs {
		domainLog := domain.Log{
			ProjectID:     project.ID,
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

	err := s.txMgr.WithTransaction(ctx, func(txCtx context.Context) error {
		return s.repo.CreateBatch(txCtx, domainLogs)
	})
	if err != nil {
		return nil, err
	}
	return domainLogs, nil
}
