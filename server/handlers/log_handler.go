package handlers

import (
	"archive/tar"
	"compress/gzip"
	"errors"
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

// Upload limits: a legitimate batch is ~100 compressed log entries, well under
// a megabyte. The caps only exist to stop abuse (huge uploads, gzip bombs).
const (
	maxUploadBytes       = 50 << 20  // compressed request body
	maxDecompressedBytes = 256 << 20 // decompressed tar stream
)

func (h *LogHandler) DumpLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := cslog.L(ctx)

	project, err := middleware.GetProjectFromContext(ctx)
	if err != nil {
		log.WithError(err).Error("Failed to get project from context")
		RespondError(w, err)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)

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

	gzr, err := gzip.NewReader(file)
	if err != nil {
		log.WithError(err).Error("Failed to create gzip reader")
		RespondError(w, utils.NewError(nil, domain.INVALID_REQUEST_DATA_ERR_CODE,
			errors.New("failed to create gzip reader")))
		return
	}
	defer gzr.Close()

	// LimitReader guards against gzip bombs: a stream that decompresses past
	// the cap yields a truncated tar, which the service rejects as an error.
	tr := tar.NewReader(io.LimitReader(gzr, maxDecompressedBytes))

	_, err = h.svc.DumpLogs(ctx, project, tr)
	if err != nil {
		log.WithError(err).Error("Failed to dump logs")
		RespondError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}
