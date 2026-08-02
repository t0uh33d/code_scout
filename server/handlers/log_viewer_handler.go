package handlers

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/internal/ports"
	"github.com/t0uh33d/code_scout/internal/services"
	"github.com/t0uh33d/code_scout/pkg/cslog"
	"github.com/t0uh33d/code_scout/pkg/search"
	"github.com/t0uh33d/code_scout/pkg/sse"
	"github.com/t0uh33d/code_scout/server/middleware"
	"github.com/t0uh33d/code_scout/view"
)

type LogViewerHandler struct {
	querySvc   *services.LogQueryService
	projectSvc ports.ProjectManager
	broker     *sse.Broker
}

func NewLogViewerHandler(querySvc *services.LogQueryService, projectSvc ports.ProjectManager, broker *sse.Broker) *LogViewerHandler {
	return &LogViewerHandler{querySvc: querySvc, projectSvc: projectSvc, broker: broker}
}

// project loads the record the shell's sidebar needs. A nil result is not fatal
// to rendering: the sidebar falls back to the project id.
func (h *LogViewerHandler) project(ctx context.Context, projectID uuid.UUID) *domain.Project {
	project, _, err := h.projectSvc.GetProject(ctx, projectID)
	if err != nil {
		cslog.L(ctx).WithError(err).Warn("Could not load project for the sidebar")
		return nil
	}
	return project
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
// Overview handles GET /project/{id}/overview — the screen you land on.
func (h *LogViewerHandler) Overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}

	project, _, err := h.projectSvc.GetProject(ctx, projectID)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	// A failed rollup renders the empty state rather than an error page: the
	// overview is a summary, and losing it should not lose the whole shell.
	stats, err := h.querySvc.GetProjectOverview(ctx, projectID)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to load project overview")
	}

	recent, err := h.querySvc.GetErrorGroups(ctx, projectID, nil, 4)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to load recent errors")
	}

	view.OverviewPage(view.OverviewData{
		User:         middleware.UserFrom(ctx),
		Project:      project,
		ProjectID:    projectID,
		Stats:        stats,
		RecentErrors: recent,
	}).Render(ctx, w)
}

// Errors handles GET /project/{id}/errors — one row per distinct problem.
func (h *LogViewerHandler) Errors(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}

	// No window: an error that stopped happening yesterday is still the thing
	// you came here to find, and the row says when it was last seen.
	groups, err := h.querySvc.GetErrorGroups(ctx, projectID, nil, 100)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to load error groups")
		http.Error(w, "Could not load errors", http.StatusInternalServerError)
		return
	}

	view.ErrorsPage(view.ErrorsData{
		User:      middleware.UserFrom(ctx),
		Project:   h.project(ctx, projectID),
		ProjectID: projectID,
		Groups:    groups,
	}).Render(ctx, w)
}

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

	// A typo in the search box used to answer 400, which threw away the whole
	// screen — including the box you would fix it in. The complaint is shown
	// inline instead and the unfiltered list stays underneath.
	var queryError string
	filter, parseErr := search.Parse(query)
	if parseErr != nil {
		queryError = parseErr.Error()
		filter = &domain.SearchFilter{}
	}

	result, err := h.querySvc.ListLogs(ctx, projectID, effectiveQuery(query, queryError), cursor, limit)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to list logs")
		http.Error(w, "Could not load logs", http.StatusInternalServerError)
		return
	}

	// Counted inside the same window the list uses, so a chip never offers a
	// tag that the current view has none of.
	tags, tagErr := h.querySvc.GetTagCounts(ctx, projectID, filter.Since, 20)
	if tagErr != nil {
		cslog.L(ctx).WithError(tagErr).Error("Failed to load tag counts")
	}

	data := view.LogViewerData{
		User:       middleware.UserFrom(ctx),
		Project:    h.project(ctx, projectID),
		ProjectID:  projectID,
		Logs:       result,
		Query:      query,
		Filter:     *filter,
		Tags:       tags,
		QueryError: queryError,
	}
	c := view.LogViewerPage(data)
	c.Render(ctx, w)
}

// effectiveQuery drops a query the parser rejected, so the page can show the
// broken text in the box while listing as though nothing were filtered.
func effectiveQuery(query, queryError string) string {
	if queryError != "" {
		return ""
	}
	return query
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

// Sessions handles GET /project/{id}/sessions — one row per app launch.
func (h *LogViewerHandler) Sessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}

	sessions, err := h.querySvc.ListSessions(ctx, projectID, nil, 200)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to list sessions")
		http.Error(w, "Could not load sessions", http.StatusInternalServerError)
		return
	}

	// The header counts are a summary. Losing them should not lose the table.
	total, users, err := h.querySvc.SessionCounts(ctx, projectID)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to count sessions")
	}

	view.SessionsPage(view.SessionsData{
		User:      middleware.UserFrom(ctx),
		Project:   h.project(ctx, projectID),
		ProjectID: projectID,
		Sessions:  sessions,
		Total:     total,
		Users:     users,
	}).Render(ctx, w)
}

// Devices handles GET /project/{id}/devices — one row per install.
func (h *LogViewerHandler) Devices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}

	devices, err := h.querySvc.ListDevices(ctx, projectID, 200)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to list devices")
		http.Error(w, "Could not load devices", http.StatusInternalServerError)
		return
	}

	view.DevicesPage(view.DevicesData{
		User:      middleware.UserFrom(ctx),
		Project:   h.project(ctx, projectID),
		ProjectID: projectID,
		Devices:   devices,
	}).Render(ctx, w)
}

// DeviceDetail handles GET /project/{id}/device/{iid} — one install and every
// session it has run.
func (h *LogViewerHandler) DeviceDetail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)

	projectID, err := uuid.Parse(vars["id"])
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}
	installationID, err := uuid.Parse(vars["iid"])
	if err != nil {
		http.Error(w, "Invalid installation ID", http.StatusBadRequest)
		return
	}

	device, err := h.querySvc.GetDevice(ctx, projectID, installationID)
	if err != nil {
		// Scoped to the project already, so a miss means it does not exist here.
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}

	sessions, err := h.querySvc.ListSessions(ctx, projectID, &installationID, 200)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to list device sessions")
		http.Error(w, "Could not load sessions", http.StatusInternalServerError)
		return
	}

	view.DeviceDetailPage(view.DeviceDetailData{
		User:      middleware.UserFrom(ctx),
		Project:   h.project(ctx, projectID),
		ProjectID: projectID,
		Device:    *device,
		Sessions:  sessions,
	}).Render(ctx, w)
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
		User:      middleware.UserFrom(ctx),
		Project:   h.project(ctx, projectID),
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
		User:      middleware.UserFrom(ctx),
		Project:   h.project(ctx, projectID),
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
