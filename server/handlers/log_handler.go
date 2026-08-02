package handlers

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"net/http"

	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/internal/ports"
	"github.com/t0uh33d/code_scout/internal/services"
	"github.com/t0uh33d/code_scout/pkg/cslog"
	"github.com/t0uh33d/code_scout/pkg/utils"
	"github.com/t0uh33d/code_scout/server/middleware"
)

type LogHandler struct {
	svc      ports.LogManager
	settings *services.InstanceSettingsService
}

func NewLogHandler(svc ports.LogManager, settings *services.InstanceSettingsService) *LogHandler {
	return &LogHandler{svc: svc, settings: settings}
}

// A legitimate batch is ~100 compressed log entries, well under a megabyte.
// The caps only exist to stop abuse (huge uploads, gzip bombs).
//
// The compressed cap is an instance setting, read per request so a change
// applies to the very next upload. The decompressed guard is not: it is the
// gzip-bomb protection, and a setting that can switch it off is the kind of
// setting that does not belong in the instance. It stays a fixed multiple of
// the largest upload the settings allow.
const (
	// decompressionRatioGuard bounds inflation relative to the configured
	// upload cap. Real batches compress at nothing like this, so anything
	// approaching it is either broken or hostile.
	decompressionRatioGuard = 5
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

	maxUploadBytes := h.settings.Current().MaxUploadBytes
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)

	err = r.ParseMultipartForm(10 << 20)
	if err != nil {
		// A body over the cap is its own answer. Reported as 400 it looks like
		// a malformed payload, and an SDK retries the same bytes forever
		// instead of sending fewer.
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			log.WithField("max_upload_bytes", maxUploadBytes).Warn("Upload over the configured cap")
			RespondError(w, utils.NewError(nil, domain.ERR_PAYLOAD_TOO_LARGE_ERR_CODE,
				errors.New(domain.ERR_PAYLOAD_TOO_LARGE_ERR)))
			return
		}
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
	tr := tar.NewReader(io.LimitReader(gzr, maxUploadBytes*decompressionRatioGuard))

	_, err = h.svc.DumpLogs(ctx, project, tr)
	if err != nil {
		log.WithError(err).Error("Failed to dump logs")
		RespondError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}
