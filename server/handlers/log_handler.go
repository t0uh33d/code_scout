package handlers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/internal/ports"
	"github.com/t0uh33d/code_scout/pkg/cslog"
	"github.com/t0uh33d/code_scout/pkg/utils"
	"github.com/t0uh33d/code_scout/server/middleware"
)

type LogHandler struct {
	svc ports.LogManager
}

func NewLogHandler(svc ports.LogManager) *LogHandler {
	return &LogHandler{svc: svc}
}

func (h *LogHandler) DumpLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := cslog.L(ctx)

	project, err := middleware.GetProjectFromContext(ctx)
	if err != nil {
		log.WithError(err).Error("Failed to get project from context")
		RespondError(w, err)
		return
	}

	err = r.ParseMultipartForm(10 << 20)
	if err != nil {
		log.WithError(err).Error("Failed to parse multipart form")
		RespondError(w, utils.NewError(nil, domain.INVALID_REQUEST_DATA_ERR_CODE,
			errors.New("failed to parse multipart form data")))
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		log.WithError(err).Error("Failed to get file from form")
		RespondError(w, utils.NewError(nil, domain.INVALID_REQUEST_DATA_ERR_CODE,
			errors.New("failed to get file from form data")))
		return
	}

	defer file.Close()

	const maxLogUploadBytes = 10 << 20
	payload, err := io.ReadAll(io.LimitReader(file, maxLogUploadBytes))
	if err != nil {
		log.WithError(err).Error("Failed to read upload")
		RespondError(w, utils.NewError(nil, domain.INVALID_REQUEST_DATA_ERR_CODE,
			errors.New("failed to read uploaded file")))
		return
	}

	if len(payload) >= 2 && payload[0] == 0x1f && payload[1] == 0x8b {
		gzr, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			log.WithError(err).Error("Failed to create gzip reader")
			RespondError(w, utils.NewError(nil, domain.INVALID_REQUEST_DATA_ERR_CODE,
				errors.New("failed to create gzip reader")))
			return
		}
		defer gzr.Close()

		tr := tar.NewReader(gzr)
		if _, err := h.svc.DumpLogs(ctx, project, tr); err != nil {
			log.WithError(err).Error("Failed to dump logs")
			RespondError(w, err)
			return
		}
	} else {
		code, err := h.svc.IngestLogsJSON(ctx, project, payload)
		if err != nil {
			if code == 400 {
				log.WithError(err).Warn("Invalid JSON log upload")
				RespondError(w, utils.NewError(nil, domain.INVALID_REQUEST_DATA_ERR_CODE,
					fmt.Errorf("file must be gzip+tar (.tar.gz) or a UTF-8 JSON array of logs: %w", err)))
				return
			}
			log.WithError(err).Error("Failed to ingest JSON logs")
			RespondError(w, err)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}
