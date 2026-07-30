package handlers

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/internal/services"
	"github.com/t0uh33d/code_scout/pkg/cslog"
	"github.com/t0uh33d/code_scout/pkg/sse"
	"github.com/t0uh33d/code_scout/view"
)

type LogViewerHandler struct {
	querySvc *services.LogQueryService
	broker   *sse.Broker
}

func NewLogViewerHandler(querySvc *services.LogQueryService, broker *sse.Broker) *LogViewerHandler {
	return &LogViewerHandler{querySvc: querySvc, broker: broker}
}

// parseLogCursor reads the keyset cursor from query params. Both parts must be
// present and valid; otherwise pagination starts from the top.
func parseLogCursor(r *http.Request) *domain.LogCursor {
	ts := r.URL.Query().Get("cursor")
	idStr := r.URL.Query().Get("cursor_id")
	if ts == "" || idStr == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return nil
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return nil
	}
	return &domain.LogCursor{Time: t, ID: id}
}

// LogViewer renders the main log viewer page for a project.
func (h *LogViewerHandler) LogViewer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)

	projectID, err := uuid.Parse(vars["id"])
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}

	query := r.URL.Query().Get("q")
	cursor := parseLogCursor(r)

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	result, err := h.querySvc.ListLogs(ctx, projectID, query, cursor, limit)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to list logs")
		http.Error(w, fmt.Sprintf("Query error: %s", err.Error()), http.StatusBadRequest)
		return
	}

	data := view.LogViewerData{
		ProjectID: projectID,
		Logs:      result,
		Query:     query,
	}
	c := view.LogViewerPage(data)
	c.Render(ctx, w)
}

// LogsPartial returns just the log rows for HTMX infinite scroll.
func (h *LogViewerHandler) LogsPartial(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)

	projectID, err := uuid.Parse(vars["id"])
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}

	query := r.URL.Query().Get("q")
	cursor := parseLogCursor(r)

	result, err := h.querySvc.ListLogs(ctx, projectID, query, cursor, 50)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to list logs partial")
		http.Error(w, "Query failed", http.StatusInternalServerError)
		return
	}

	c := view.LogRows(result, projectID, query)
	c.Render(ctx, w)
}

// SessionTimeline renders the session timeline page.
func (h *LogViewerHandler) SessionTimeline(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)

	projectID, err := uuid.Parse(vars["id"])
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}
	sessionID, err := uuid.Parse(vars["sid"])
	if err != nil {
		http.Error(w, "Invalid session ID", http.StatusBadRequest)
		return
	}

	logs, err := h.querySvc.GetSessionTimeline(ctx, projectID, sessionID)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to get session timeline")
		http.Error(w, "Failed to load session", http.StatusInternalServerError)
		return
	}

	data := view.SessionTimelineData{
		ProjectID: projectID,
		SessionID: sessionID,
		Logs:      logs,
	}
	c := view.SessionTimelinePage(data)
	c.Render(ctx, w)
}

// NetworkDetail renders the network request detail page.
func (h *LogViewerHandler) NetworkDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)

	projectID, err := uuid.Parse(vars["id"])
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}
	requestID, err := uuid.Parse(vars["rid"])
	if err != nil {
		http.Error(w, "Invalid request ID", http.StatusBadRequest)
		return
	}

	logs, err := h.querySvc.GetNetworkRequest(ctx, projectID, requestID)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to get network request")
		http.Error(w, "Failed to load request", http.StatusInternalServerError)
		return
	}

	data := view.NetworkDetailData{
		ProjectID: projectID,
		RequestID: requestID,
		Logs:      logs,
	}
	c := view.NetworkDetailPage(data)
	c.Render(ctx, w)
}

// StreamLogs handles the SSE endpoint for real-time log streaming.
func (h *LogViewerHandler) StreamLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := cslog.L(ctx)

	projectIDStr := r.URL.Query().Get("project_id")
	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		http.Error(w, "Invalid project_id", http.StatusBadRequest)
		return
	}

	// Disable write timeout for SSE
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		log.WithError(err).Warn("Failed to disable write deadline for SSE")
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Subscribe to the broker
	ch, err := h.broker.Subscribe(projectID)
	if err != nil {
		log.WithError(err).Error("SSE: failed to subscribe")
		http.Error(w, "Too many viewers", http.StatusTooManyRequests)
		return
	}
	defer h.broker.Unsubscribe(projectID, ch)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\ndata: {\"project_id\":\"%s\"}\n\n", projectID)
	flusher.Flush()

	log.WithField("project_id", projectID).Info("SSE: client connected")

	for {
		select {
		case <-ctx.Done():
			log.Info("SSE: client disconnected")
			return
		case logs, ok := <-ch:
			if !ok {
				log.Info("SSE: channel closed")
				return
			}
			// Render each log as an HTML fragment via templ. SSE data fields
			// are line-delimited, so multi-line HTML must be re-framed with a
			// "data: " prefix per line or the first newline ends the event.
			for _, l := range logs {
				var buf bytes.Buffer
				if err := view.SSELogRow(l).Render(ctx, &buf); err != nil {
					log.WithError(err).Warn("SSE: failed to render log row")
					continue
				}
				fmt.Fprintf(w, "event: log\n")
				for _, line := range strings.Split(buf.String(), "\n") {
					fmt.Fprintf(w, "data: %s\n", line)
				}
				fmt.Fprintf(w, "\n")
			}
			flusher.Flush()
		}
	}
}

// ProjectStats returns sparkline stats as an HTMX partial.
func (h *LogViewerHandler) ProjectStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)

	projectID, err := uuid.Parse(vars["id"])
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}

	stats, err := h.querySvc.GetLogStats(ctx, projectID, 24)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to get stats")
		// Return empty sparkline on error
		w.WriteHeader(http.StatusOK)
		return
	}

	c := view.SparklineStats(stats)
	c.Render(ctx, w)
}
