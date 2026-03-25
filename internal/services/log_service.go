package services

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/internal/ports"
	"github.com/t0uh33d/code_scout/pkg/cslog"
)

type LogService struct {
	repo  ports.LogRepository
	txMgr ports.TransactionManager
}

func NewLogService(repo ports.LogRepository, txMgr ports.TransactionManager) *LogService {
	return &LogService{
		repo:  repo,
		txMgr: txMgr,
	}
}

// skipTarLogEntry ignores macOS AppleDouble (._*) and resource-fork paths that are not JSON log batches.
func skipTarLogEntry(name string) bool {
	if strings.Contains(name, "__MACOSX/") {
		return true
	}
	base := filepath.Base(name)
	return strings.HasPrefix(base, "._")
}

func (s *LogService) DumpLogs(ctx context.Context, project *domain.Project, tr *tar.Reader) (int, error) {
	log := cslog.L(ctx)
	log.Info("Dumping logs...")

	var allLogs []domain.IncomingLog
	for {
		header, err := tr.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			log.WithError(err).Error("Failed to read tar entry")
			return 500, err
		}

		if header.Typeflag != tar.TypeReg {
			log.WithField("file", header.Name).Warn("Skipping non-regular file")
			continue
		}

		lim := io.LimitReader(tr, header.Size)
		body, err := io.ReadAll(lim)
		if err != nil {
			log.WithError(err).WithField("file", header.Name).Error("Failed to read tar member")
			return 500, err
		}

		if skipTarLogEntry(header.Name) {
			log.WithField("file", header.Name).Debug("Skipping non-log tar member")
			continue
		}

		var rawLogs []domain.IncomingLog
		if err := json.Unmarshal(body, &rawLogs); err != nil {
			log.WithError(err).WithField("file", header.Name).Warn("Skipping tar member: not a JSON log array")
			continue
		}

		allLogs = append(allLogs, rawLogs...)
	}

	if err := s.insertIncomingLogs(ctx, allLogs); err != nil {
		log.WithError(err).Error("Failed to insert incoming logs")
		return 500, err
	}

	return 200, nil
}

// IngestLogsJSON accepts a raw JSON body: [ {...}, ... ] matching domain.IncomingLog.
func (s *LogService) IngestLogsJSON(ctx context.Context, project *domain.Project, payload []byte) (int, error) {
	_ = project
	log := cslog.L(ctx)
	log.Info("Ingesting logs from JSON...")

	payload = stripUTF8BOM(payload)
	var logs []domain.IncomingLog
	if err := json.Unmarshal(payload, &logs); err != nil {
		log.WithError(err).Error("Failed to decode JSON log array")
		return 400, err
	}

	if err := s.insertIncomingLogs(ctx, logs); err != nil {
		log.WithError(err).Error("Failed to insert incoming logs")
		return 500, err
	}
	return 200, nil
}

func stripUTF8BOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}

func (s *LogService) insertIncomingLogs(ctx context.Context, logs []domain.IncomingLog) error {
	batchSize := 1000
	numWorkers := 5

	ch := make(chan []domain.IncomingLog)
	errCh := make(chan error, numWorkers)

	var wg sync.WaitGroup

	// 🔹 Producer: create batches
	go func() {
		for i := 0; i < len(logs); i += batchSize {
			end := i + batchSize
			if end > len(logs) {
				end = len(logs)
			}
			ch <- logs[i:end]
		}
		close(ch)
	}()

	// 🔹 Workers
	wg.Add(numWorkers)

	for i := 0; i < numWorkers; i++ {
		go func() {
			defer wg.Done()

			for batch := range ch {

				domainLogs := make([]domain.Log, 0, len(batch))

				for _, log := range batch {
					// Omit client "id": server-generated PK avoids duplicate key on re-upload of same payload.
					domainLogs = append(domainLogs, domain.Log{
						SessionID:     log.SessionID,
						Level:         log.Level,
						Message:       log.Message,
						Error:         log.Error,
						StackTrace:    log.StackTrace,
						Metadata:      log.Metadata,
						Tags:          log.Tags,
						TimeStamp:     log.Timestamp,
						IsNetworkCall: bool(log.IsNetworkCall),
						RequestID:     log.RequestID,
						CallPhase:     log.CallPhase,
					})
				}

				err := s.txMgr.WithTransaction(ctx, func(txCtx context.Context) error {
					return s.repo.CreateBatch(txCtx, domainLogs)
				})

				if err != nil {
					errCh <- err
					return
				}
			}
		}()
	}

	// 🔹 Wait for workers
	wg.Wait()
	close(errCh)

	// 🔹 Check errors
	for err := range errCh {
		if err != nil {
			return err
		}
	}

	return nil
}
