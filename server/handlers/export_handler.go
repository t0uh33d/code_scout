package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/getcodescout/code_scout/internal/services"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"github.com/getcodescout/code_scout/pkg/search"
)

type ExportHandler struct {
	querySvc *services.LogQueryService
}

func NewExportHandler(querySvc *services.LogQueryService) *ExportHandler {
	return &ExportHandler{querySvc: querySvc}
}

// ExportLogs handles CSV or JSON log export as a streaming download.
func (h *ExportHandler) ExportLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := cslog.L(ctx)

	projectIDStr := r.URL.Query().Get("project_id")
	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		http.Error(w, "Invalid project_id", http.StatusBadRequest)
		return
	}

	format := r.URL.Query().Get("fmt")
	if format == "" {
		format = "csv"
	}

	query := r.URL.Query().Get("q")

	// Parsed here, before any header is set, because everything below streams
	// straight into the ResponseWriter: once the first byte is out the status is
	// decided, and a failure part way through can only stop writing. A rejected
	// query used to take that path and answer 200 with an empty body, which is
	// indistinguishable from "nothing matched" — the export looked like it had
	// simply found no logs.
	if _, err := search.Parse(query); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	timestamp := time.Now().Format("2006-01-02")

	switch format {
	case "csv":
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"codescout-logs-%s.csv\"", timestamp))

		if err := h.querySvc.ExportLogsCSV(ctx, projectID, query, w); err != nil {
			log.WithError(err).Error("Export CSV failed")
			return
		}

	case "json":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"codescout-logs-%s.json\"", timestamp))

		if err := h.querySvc.ExportLogsJSON(ctx, projectID, query, w); err != nil {
			log.WithError(err).Error("Export JSON failed")
			return
		}

	default:
		http.Error(w, "Invalid format. Use 'csv' or 'json'.", http.StatusBadRequest)
	}
}
