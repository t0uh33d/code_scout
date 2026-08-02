package services

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/internal/ports"
	"github.com/t0uh33d/code_scout/pkg/cslog"
	"github.com/t0uh33d/code_scout/pkg/search"
)

type LogQueryService struct {
	repo ports.LogRepository
}

func NewLogQueryService(repo ports.LogRepository) *LogQueryService {
	return &LogQueryService{repo: repo}
}

// ListLogs queries logs with search, filtering, and cursor-based pagination.
func (s *LogQueryService) ListLogs(ctx context.Context, projectID uuid.UUID, query string, cursor *domain.LogCursor, limit int) (*domain.LogListResult, error) {
	log := cslog.L(ctx)

	var filter domain.SearchFilter
	if query != "" {
		parsed, err := search.Parse(query)
		if err != nil {
			return nil, fmt.Errorf("invalid search query: %w", err)
		}
		filter = *parsed
	}

	opts := domain.LogListOpts{
		ProjectID: projectID,
		Filter:    filter,
		Cursor:    cursor,
		Limit:     limit,
	}

	result, err := s.repo.List(ctx, opts)
	if err != nil {
		log.WithError(err).Error("Failed to list logs")
		return nil, err
	}

	return result, nil
}

// GetSessionTimeline returns all logs for a session in chronological order.
func (s *LogQueryService) GetSessionTimeline(ctx context.Context, projectID, sessionID uuid.UUID) ([]domain.Log, error) {
	log := cslog.L(ctx)

	logs, err := s.repo.GetBySessionID(ctx, projectID, sessionID, 500)
	if err != nil {
		log.WithError(err).Error("Failed to get session timeline")
		return nil, err
	}

	return logs, nil
}

// GetNetworkRequest returns all log phases for a network request.
func (s *LogQueryService) GetNetworkRequest(ctx context.Context, projectID uuid.UUID, requestID uuid.UUID) ([]domain.Log, error) {
	log := cslog.L(ctx)

	logs, err := s.repo.GetByRequestID(ctx, projectID, requestID)
	if err != nil {
		log.WithError(err).Error("Failed to get network request")
		return nil, err
	}

	return logs, nil
}

// GetLogStats returns aggregated log statistics for sparkline display.
func (s *LogQueryService) GetLogStats(ctx context.Context, projectID uuid.UUID, hours int) (*domain.LogStatsResult, error) {
	log := cslog.L(ctx)

	result, err := s.repo.GetStats(ctx, domain.LogStatsOpts{
		ProjectID: projectID,
		Hours:     hours,
	})
	if err != nil {
		log.WithError(err).Error("Failed to get log stats")
		return nil, err
	}

	return result, nil
}

// GetProjectOverview returns the counts behind the overview screen.
func (s *LogQueryService) GetProjectOverview(ctx context.Context, projectID uuid.UUID) (*domain.ProjectOverview, error) {
	overview, err := s.repo.GetOverview(ctx, projectID)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to get project overview")
		return nil, err
	}
	return overview, nil
}

// GetTagCounts lists the tags in use on a project, for the log viewer's picker.
func (s *LogQueryService) GetTagCounts(ctx context.Context, projectID uuid.UUID, since *time.Time, limit int) ([]domain.TagCount, error) {
	counts, err := s.repo.GetTagCounts(ctx, projectID, since, limit)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to get tag counts")
		return nil, err
	}
	return counts, nil
}

// ExportLogsCSV streams logs as CSV to the provided writer.
func (s *LogQueryService) ExportLogsCSV(ctx context.Context, projectID uuid.UUID, query string, w io.Writer) error {
	log := cslog.L(ctx)

	var filter domain.SearchFilter
	if query != "" {
		parsed, err := search.Parse(query)
		if err != nil {
			return fmt.Errorf("invalid search query: %w", err)
		}
		filter = *parsed
	}

	csvWriter := csv.NewWriter(w)
	defer csvWriter.Flush()

	// Write header
	if err := csvWriter.Write([]string{
		"id", "session_id", "level", "message", "error", "timestamp",
		"is_network_call", "request_id", "call_phase", "tags", "metadata",
	}); err != nil {
		return err
	}

	var cursor *domain.LogCursor
	for {
		result, err := s.repo.List(ctx, domain.LogListOpts{
			ProjectID: projectID,
			Filter:    filter,
			Cursor:    cursor,
			Limit:     200,
		})
		if err != nil {
			log.WithError(err).Error("Export: failed to fetch page")
			return err
		}

		for _, l := range result.Items {
			errStr := ""
			if l.Error != nil {
				errStr = *l.Error
			}
			reqID := ""
			if l.RequestID != nil {
				reqID = l.RequestID.String()
			}
			phase := ""
			if l.CallPhase != nil {
				phase = string(*l.CallPhase)
			}
			tags := ""
			if l.Tags != nil {
				tags = string(*l.Tags)
			}
			meta := ""
			if l.Metadata != nil {
				meta = string(*l.Metadata)
			}

			if err := csvWriter.Write([]string{
				l.ID.String(),
				l.SessionID.String(),
				l.Level,
				l.Message,
				errStr,
				l.TimeStamp.Format(time.RFC3339),
				fmt.Sprintf("%v", l.IsNetworkCall),
				reqID,
				phase,
				tags,
				meta,
			}); err != nil {
				return err
			}
		}

		csvWriter.Flush()

		if !result.HasMore {
			break
		}
		cursor = result.NextCursor
	}

	return nil
}

// exportLog is the JSON export shape: snake_case keys matching the ingestion
// contract (IncomingLog / CSV header), no server-internal fields.
type exportLog struct {
	ID            uuid.UUID         `json:"id"`
	SessionID     uuid.UUID         `json:"session_id"`
	Level         string            `json:"level"`
	Message       string            `json:"message"`
	Error         *string           `json:"error"`
	StackTrace    *json.RawMessage  `json:"stack_trace"`
	Metadata      *json.RawMessage  `json:"metadata"`
	Tags          *json.RawMessage  `json:"tags"`
	TimeStamp     time.Time         `json:"timestamp"`
	IsNetworkCall bool              `json:"is_network_call"`
	RequestID     *uuid.UUID        `json:"request_id"`
	CallPhase     *domain.CallPhase `json:"call_phase"`
}

// ExportLogsJSON streams logs as a JSON array to the provided writer.
func (s *LogQueryService) ExportLogsJSON(ctx context.Context, projectID uuid.UUID, query string, w io.Writer) error {
	log := cslog.L(ctx)

	var filter domain.SearchFilter
	if query != "" {
		parsed, err := search.Parse(query)
		if err != nil {
			return fmt.Errorf("invalid search query: %w", err)
		}
		filter = *parsed
	}

	// Write opening bracket
	if _, err := w.Write([]byte("[\n")); err != nil {
		return err
	}

	var cursor *domain.LogCursor
	first := true
	for {
		result, err := s.repo.List(ctx, domain.LogListOpts{
			ProjectID: projectID,
			Filter:    filter,
			Cursor:    cursor,
			Limit:     200,
		})
		if err != nil {
			log.WithError(err).Error("Export JSON: failed to fetch page")
			return err
		}

		for _, l := range result.Items {
			// Marshal before writing so a bad row (e.g. invalid RawMessage)
			// is skipped instead of truncating the stream mid-array.
			data, err := json.MarshalIndent(exportLog{
				ID:            l.ID,
				SessionID:     l.SessionID,
				Level:         l.Level,
				Message:       l.Message,
				Error:         l.Error,
				StackTrace:    l.StackTrace,
				Metadata:      l.Metadata,
				Tags:          l.Tags,
				TimeStamp:     l.TimeStamp,
				IsNetworkCall: l.IsNetworkCall,
				RequestID:     l.RequestID,
				CallPhase:     l.CallPhase,
			}, "  ", "  ")
			if err != nil {
				log.WithError(err).WithField("log_id", l.ID).Warn("Export JSON: skipping unmarshalable row")
				continue
			}
			if !first {
				if _, err := w.Write([]byte(",\n")); err != nil {
					return err
				}
			}
			if _, err := w.Write([]byte("  ")); err != nil {
				return err
			}
			if _, err := w.Write(data); err != nil {
				return err
			}
			first = false
		}

		if !result.HasMore {
			break
		}
		cursor = result.NextCursor
	}

	_, err := w.Write([]byte("\n]\n"))
	return err
}
