package handlers

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/sirupsen/logrus"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/ports"
	"github.com/getcodescout/code_scout/internal/services"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"github.com/getcodescout/code_scout/pkg/utils"
	"github.com/getcodescout/code_scout/server/middleware"
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

	// maxDecompressedBytes is an absolute ceiling on top of that ratio, and is
	// the one that actually binds.
	//
	// The ratio alone is a multiple of a setting chosen to be generous about
	// upload size, and those are different jobs. At the default 50 MB cap it
	// permits 250 MB of inflated tar, and DumpLogs accumulates the whole thing
	// into one []IncomingLog before it writes a row, where JSON decoded into Go
	// structs costs several times its serialised size. A quarter-megabyte of
	// well-crafted zeros reaches that ceiling, so a caller holding any valid
	// project credential could ask a small instance for a gigabyte of heap, and
	// again concurrently.
	//
	// 64 MB is far above legitimate traffic and far below trouble: the SDK
	// batches on the order of a hundred entries, which is under a megabyte
	// compressed and single-digit megabytes inflated. Raising the upload cap
	// past this does not raise this.
	maxDecompressedBytes int64 = 64 << 20
)

// decompressionLimit is how much inflated tar one upload may produce.
func decompressionLimit(maxUploadBytes int64) int64 {
	if limit := maxUploadBytes * decompressionRatioGuard; limit < maxDecompressedBytes {
		return limit
	}
	return maxDecompressedBytes
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

	maxUploadBytes := h.settings.Current().MaxUploadBytes
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)

	// MultipartReader, not ParseMultipartForm.
	//
	// The difference is not where the cap fires. MaxBytesReader bounds the read
	// either way, so an oversized upload is refused at the same byte under both
	// and neither reads a 500 MB body. What ParseMultipartForm does is buffer
	// the *whole* part before returning: up to its 10 MB threshold in memory,
	// and into a temp file on disk past that. Every upload therefore paid for
	// itself twice — once to spool, once to decode — and a legitimate batch
	// arriving at the cap wrote a temp file for no reason.
	//
	// Streaming the part means decoding starts on the first bytes and peak
	// memory is a few buffers rather than the batch, which is what matters when
	// a production app's worth of devices upload at once. Nothing touches disk.
	mr, err := r.MultipartReader()
	if err != nil {
		log.WithError(err).Error("Not a multipart upload")
		RespondError(w, utils.NewError(nil, domain.INVALID_REQUEST_DATA_ERR_CODE,
			errors.New("failed to parse multipart form data")))
		return
	}

	part, err := nextFilePart(mr)
	if err != nil {
		if h.respondIfTooLarge(w, log, err, maxUploadBytes) {
			return
		}
		log.WithError(err).Error("Failed to read the file part")
		RespondError(w, utils.NewError(nil, domain.INVALID_REQUEST_DATA_ERR_CODE,
			errors.New("failed to get file from form data")))
		return
	}
	defer part.Close()

	// Streaming moves the cap error from one place to many: it can now surface
	// from the gzip header, from a tar entry, or from the middle of a JSON
	// decode deep inside the service, which wraps it into something unrecognisable.
	// This watches the one reader every one of those paths pulls through, so
	// the answer is decided by what actually happened rather than by whichever
	// layer happened to report it.
	watched := &readErrorWatcher{r: part}

	gzr, err := gzip.NewReader(watched)
	if err != nil {
		if h.respondIfTooLarge(w, log, watched.err, maxUploadBytes) {
			return
		}
		log.WithError(err).Error("Failed to create gzip reader")
		RespondError(w, utils.NewError(nil, domain.INVALID_REQUEST_DATA_ERR_CODE,
			errors.New("failed to create gzip reader")))
		return
	}
	defer gzr.Close()

	// LimitReader guards against gzip bombs: a stream that decompresses past
	// the cap yields a truncated tar, which the service rejects as an error.
	tr := tar.NewReader(io.LimitReader(gzr, decompressionLimit(maxUploadBytes)))

	_, err = h.svc.DumpLogs(ctx, project, tr)
	if err != nil {
		if h.respondIfTooLarge(w, log, watched.err, maxUploadBytes) {
			return
		}
		log.WithError(err).Error("Failed to dump logs")
		RespondError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// respondIfTooLarge answers 413 when err is the upload cap being hit, and
// reports whether it did.
//
// A body over the cap is its own answer. Reported as 400 it looks like a
// malformed payload, and an SDK retries the same bytes forever instead of
// sending fewer.
func (h *LogHandler) respondIfTooLarge(w http.ResponseWriter, log *logrus.Entry, err error, maxUploadBytes int64) bool {
	var tooLarge *http.MaxBytesError
	if !errors.As(err, &tooLarge) {
		return false
	}
	log.WithField("max_upload_bytes", maxUploadBytes).Warn("Upload over the configured cap")
	RespondError(w, utils.NewError(nil, domain.ERR_PAYLOAD_TOO_LARGE_ERR_CODE,
		errors.New(domain.ERR_PAYLOAD_TOO_LARGE_ERR)))
	return true
}

// nextFilePart walks to the part named "file", skipping anything else rather
// than assuming it comes first. The published SDK sends only that one field,
// but a multipart body has no rule saying it must.
func nextFilePart(mr *multipart.Reader) (*multipart.Part, error) {
	for {
		part, err := mr.NextPart()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, errors.New("no file field in the upload")
			}
			return nil, err
		}
		if part.FormName() == "file" {
			return part, nil
		}
		part.Close()
	}
}

// readErrorWatcher remembers the first real read error to pass through it.
//
// Necessary because the reader is handed down through gzip, tar and a JSON
// decoder before anyone acts on a failure, and each of those is entitled to
// wrap or replace the error it saw. The original is what decides whether this
// was a 413 or a 400.
type readErrorWatcher struct {
	r   io.Reader
	err error
}

func (w *readErrorWatcher) Read(p []byte) (int, error) {
	n, err := w.r.Read(p)
	if err != nil && w.err == nil && !errors.Is(err, io.EOF) {
		w.err = err
	}
	return n, err
}
