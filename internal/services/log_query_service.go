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
func (s *LogQueryService) ListLogs(ctx context.Context, projectID uuid.UUID, query string, cursor *time.Time, limit int) (*domain.LogListResult, error) {
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

	var cursor *time.Time
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

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")

	var cursor *time.Time
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
			if !first {
				if _, err := w.Write([]byte(",\n")); err != nil {
					return err
				}
			}
			if err := encoder.Encode(l); err != nil {
				return err
			}
			first = false
		}

		if !result.HasMore {
			break
		}
		cursor = result.NextCursor
	}

	_, err := w.Write([]byte("\n]"))
	return err
}
