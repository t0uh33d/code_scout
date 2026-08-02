package services

import (
	"archive/tar"
	"context"
	"encoding/json"
	"path"
	"time"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/internal/ports"
	"github.com/t0uh33d/code_scout/pkg/cslog"
)

type LogService struct {
	repo      ports.LogRepository
	txMgr     ports.TransactionManager
	publisher ports.EventPublisher // nil-safe: if nil, no SSE events are published
	// sessions is nil-safe too, so a batch from an SDK that sends none still
	// ingests exactly as before.
	sessions ports.SessionRepository
}

func NewLogService(repo ports.LogRepository, txMgr ports.TransactionManager, publisher ports.EventPublisher, sessions ports.SessionRepository) *LogService {
	return &LogService{
		repo:      repo,
		txMgr:     txMgr,
		publisher: publisher,
		sessions:  sessions,
	}
}

func (s *LogService) DumpLogs(ctx context.Context, project *domain.Project, tr *tar.Reader) (int, error) {
	log := cslog.L(ctx)
	log.Info("Dumping logs...")

	// Entries are read by name, not position. The published SDK sends one
	// data.json and keeps working untouched; a newer one adds sessions.json
	// beside it. Anything unrecognised is skipped rather than refused, so a
	// future SDK can add a file without this server rejecting the whole batch.
	var allLogs []domain.IncomingLog
	var allSessions []domain.IncomingSession
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

		switch path.Base(header.Name) {
		case "sessions.json":
			var sessions []domain.IncomingSession
			if err := json.NewDecoder(tr).Decode(&sessions); err != nil {
				log.WithError(err).Error("Failed to decode session file")
				return 500, err
			}
			allSessions = append(allSessions, sessions...)
		case "data.json":
			var rawLogs []domain.IncomingLog
			if err := json.NewDecoder(tr).Decode(&rawLogs); err != nil {
				log.WithError(err).Error("Failed to decode log file")
				return 500, err
			}
			allLogs = append(allLogs, rawLogs...)
		default:
			log.WithField("file", header.Name).Warn("Skipping unrecognised entry")
		}
	}

	// Sessions first: the logs that follow reference them, and a session that
	// fails to record should not take its logs down with it — the logs are the
	// payload, the session is context.
	s.upsertSessions(ctx, project, allSessions)

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
		// A client that sends no id decodes to uuid.Nil. Leaving that as the
		// client id would make every such log collide on the unique index and
		// silently drop all but the first, so it stays null and simply is not
		// deduplicated.
		var clientID *uuid.UUID
		if logEntry.ID != uuid.Nil {
			id := logEntry.ID
			clientID = &id
		}

		domainLog := domain.Log{
			ClientID:      clientID,
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
		if domainLog.IsNetworkCall {
			domainLog.Method, domainLog.URL, domainLog.StatusCode = domain.ExtractNetworkMeta(logEntry.Metadata)
		}
		// Computed once here rather than on every read: the Errors screen
		// groups on this, and normalising thousands of messages per query
		// would be a table scan every time.
		if fp := domain.Fingerprint(domainLog.Level, domainLog.Message, domainLog.IsNetworkCall,
			domainLog.Method, domainLog.URL); fp != "" {
			domainLog.Fingerprint = &fp
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

// upsertSessions records the session context that came with a batch. Failures
// are logged and swallowed: losing the device details is worse than losing
// nothing, but it is far better than rejecting the logs themselves.
func (s *LogService) upsertSessions(ctx context.Context, project *domain.Project, incoming []domain.IncomingSession) {
	if s.sessions == nil || len(incoming) == 0 {
		return
	}
	log := cslog.L(ctx)
	now := time.Now()

	for _, in := range incoming {
		if in.ID == uuid.Nil {
			log.Warn("Skipping a session with no id")
			continue
		}
		session := in.ToSession(project.ID, now)
		if err := s.sessions.Upsert(ctx, &session); err != nil {
			log.WithError(err).WithField("session_id", in.ID).Error("Failed to record session")
		}
	}
}
