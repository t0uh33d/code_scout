package handlers

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/services"
	"github.com/getcodescout/code_scout/server/middleware"
	"github.com/google/uuid"
)

// What reading the upload as a stream changes is *when* the body is consumed,
// not whether the cap works. MaxBytesReader bounds the read either way, so an
// oversized upload is refused at the same byte under both implementations —
// that is worth pinning, but it does not tell them apart.
//
// The difference ParseMultipartForm makes is that it buffers the entire part,
// on disk past its 10 MB threshold, *before* returning. So the test that
// actually distinguishes them measures how much of the body has been read at
// the moment decoding starts: streaming begins on the first few kilobytes,
// spooling begins only once the last byte has landed.

const testUploadCap = 8 << 20 // 8 MB, so a legitimate upload can be sizeable

type stubSettingsRepo struct{ settings domain.InstanceSettings }

func (s *stubSettingsRepo) Get(context.Context) (*domain.InstanceSettings, error) {
	out := s.settings
	return &out, nil
}

func (s *stubSettingsRepo) Save(context.Context, *domain.InstanceSettings) error { return nil }

// stubLogManager records what reached the service, so a test can tell "refused
// before the service ran" from "the service accepted an empty batch".
type stubLogManager struct {
	called   bool
	logCount int
	err      error

	// counter is the reader under the request body, and bytesReadAtEntry is
	// what it had consumed the moment decoding began. That number is the whole
	// point of this change.
	counter          *countingReader
	bytesReadAtEntry int64
}

func (s *stubLogManager) DumpLogs(ctx context.Context, project *domain.Project, tr *tar.Reader) (int, error) {
	s.called = true
	if s.counter != nil {
		s.bytesReadAtEntry = s.counter.read
	}
	if s.err != nil {
		return 500, s.err
	}
	// Drain it the way the real service does, so the read errors these tests
	// are about actually surface.
	for {
		header, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return 500, err
		}
		var logs []domain.IncomingLog
		if err := json.NewDecoder(tr).Decode(&logs); err != nil {
			return 500, err
		}
		_ = header
		s.logCount += len(logs)
	}
	return s.logCount, nil
}

// countingReader is the measurement. It sits under the request body and counts
// everything the handler pulls through it.
type countingReader struct {
	r    io.Reader
	read int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.read += int64(n)
	return n, err
}

func newLogHandler(t *testing.T, svc *stubLogManager) *LogHandler {
	t.Helper()
	settings := services.NewInstanceSettingsService(&stubSettingsRepo{
		settings: domain.InstanceSettings{
			Timezone:       domain.DefaultTimezone,
			MaxUploadBytes: testUploadCap,
		},
	})
	if err := settings.Load(context.Background()); err != nil {
		t.Fatalf("load settings: %v", err)
	}
	return NewLogHandler(svc, settings)
}

// multipartBody wraps payload in the same multipart envelope the SDK sends.
func multipartBody(t *testing.T, field string, payload []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile(field, "logs.tar.gz")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &body, w.FormDataContentType()
}

// tarGz builds a real archive, so the happy path exercises the same decoding
// the SDK's uploads go through.
func tarGz(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range entries {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o600, Size: int64(len(content)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// serve runs the handler against a request whose body is counted on the way in.
func serve(t *testing.T, h *LogHandler, body *bytes.Buffer, contentType string) (*httptest.ResponseRecorder, *countingReader) {
	t.Helper()
	counter := &countingReader{r: bytes.NewReader(body.Bytes())}
	if svc, ok := h.svc.(*stubLogManager); ok {
		svc.counter = counter
	}
	req := httptest.NewRequest(http.MethodPost, "/api/logs/dump", counter)
	req.Header.Set("Content-Type", contentType)

	ctx := middleware.WithProject(req.Context(), &domain.Project{
		ID: uuid.New(), Name: "test", SessionSampleRate: domain.FullSampleRate,
	})

	rec := httptest.NewRecorder()
	h.DumpLogs(rec, req.WithContext(ctx))
	return rec, counter
}

// hugeArchive builds a real tar.gz whose *compressed* size comfortably exceeds
// the cap.
//
// The content has to be incompressible or none of this works: an earlier test
// elsewhere in this repo built a "4 MB" payload out of repeated text, which
// gzip flattened to a few kilobytes, and the test passed for entirely the wrong
// reason. Random bytes hex-encoded into JSON strings are what an oversized
// batch of real log messages looks like to gzip.
func hugeArchive(t *testing.T, compressedAtLeast int) []byte {
	t.Helper()

	// Grows gently. Hex halves under gzip, so starting at the target and
	// stepping by half gets there in a couple of rounds without building a
	// hundred megabytes to clear an eight megabyte bar.
	var archive []byte
	for size := compressedAtLeast; ; size = size * 3 / 2 {
		noise := make([]byte, size)
		if _, err := rand.Read(noise); err != nil {
			t.Fatalf("random: %v", err)
		}
		logs, err := json.Marshal([]map[string]any{
			{"id": uuid.NewString(), "level": "info", "message": hex.EncodeToString(noise)},
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		archive = tarGz(t, map[string][]byte{"data.json": logs})
		if len(archive) > compressedAtLeast {
			return archive
		}
	}
}

// The load-bearing test, and the only one that separates streaming from
// spooling. Revert to ParseMultipartForm and it fails: decoding does not start
// until the last byte of the upload has been buffered.
func TestDecodingStartsBeforeTheBodyIsFullyRead(t *testing.T) {
	svc := &stubLogManager{}
	h := newLogHandler(t, svc)

	// Comfortably under the cap, so this is an ordinary accepted upload rather
	// than anything about refusal.
	payload := hugeArchive(t, testUploadCap/4)
	body, contentType := multipartBody(t, "file", payload)

	rec, counter := serve(t, h, body, contentType)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !svc.called {
		t.Fatal("the service never ran")
	}

	// A generous bound: streaming reaches the service after a few buffers,
	// spooling reaches it after the entire body. Anything close to the payload
	// size means the upload is being buffered before it is processed.
	if svc.bytesReadAtEntry > counter.read/2 {
		t.Errorf("decoding began only after %d of %d bytes had been read. "+
			"The upload is being spooled in full before it is processed",
			svc.bytesReadAtEntry, counter.read)
	}
	t.Logf("decoding began after %d of %d bytes", svc.bytesReadAtEntry, counter.read)
}

// The cap still works. This passes under both implementations — MaxBytesReader
// is what enforces it — but it is the behaviour an SDK depends on, so it is
// pinned regardless of which side of the change it belongs to.
func TestAnOversizedUploadIsRefusedWithoutReadingItAll(t *testing.T) {
	svc := &stubLogManager{}
	h := newLogHandler(t, svc)

	// A genuine oversized batch is valid gzip — the SDK compresses before it
	// uploads. Sending noise instead would be refused by the gzip header after
	// a handful of bytes and would prove nothing about the cap.
	payload := hugeArchive(t, testUploadCap+testUploadCap/4)
	body, contentType := multipartBody(t, "file", payload)

	rec, counter := serve(t, h, body, contentType)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413: %s", rec.Code, rec.Body.String())
	}

	// The real assertion. Some overshoot is expected — MaxBytesReader lets the
	// read that crosses the limit finish — but it has to be a fraction of the
	// payload, not all of it.
	if counter.read > testUploadCap*2 {
		t.Errorf("read %d bytes of a %d byte body before refusing it; the cap is %d. "+
			"The upload is being spooled before its size is checked",
			counter.read, len(payload), testUploadCap)
	}
	t.Logf("refused after reading %d of %d bytes", counter.read, len(payload))
}

// Garbage is refused too, and just as cheaply. Nothing about being unreadable
// should make an upload expensive to reject.
func TestGarbageIsRejectedWithoutReadingItAll(t *testing.T) {
	svc := &stubLogManager{}
	h := newLogHandler(t, svc)

	noise := make([]byte, testUploadCap*2)
	if _, err := rand.Read(noise); err != nil {
		t.Fatalf("random: %v", err)
	}
	body, contentType := multipartBody(t, "file", noise)

	rec, counter := serve(t, h, body, contentType)

	if rec.Code == http.StatusOK {
		t.Fatal("a body that is not a gzip stream was accepted")
	}
	if svc.called {
		t.Error("the service ran on a body that is not a gzip stream")
	}
	if counter.read > testUploadCap*2 {
		t.Errorf("read %d bytes of a %d byte body before rejecting it", counter.read, len(noise))
	}
	t.Logf("rejected after reading %d of %d bytes", counter.read, len(noise))
}

func TestAnUploadUnderTheCapIsAccepted(t *testing.T) {
	svc := &stubLogManager{}
	h := newLogHandler(t, svc)

	logs, err := json.Marshal([]map[string]any{
		{"id": uuid.NewString(), "level": "info", "message": "hello"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body, contentType := multipartBody(t, "file", tarGz(t, map[string][]byte{"data.json": logs}))

	rec, _ := serve(t, h, body, contentType)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !svc.called {
		t.Error("a legitimate upload never reached the service")
	}
	if svc.logCount != 1 {
		t.Errorf("service saw %d logs, want 1", svc.logCount)
	}
}

// The field is found by name rather than by position. A multipart body has no
// rule saying "file" comes first.
func TestTheFileFieldIsFoundAfterOtherFields(t *testing.T) {
	svc := &stubLogManager{}
	h := newLogHandler(t, svc)

	logs, _ := json.Marshal([]map[string]any{{"id": uuid.NewString(), "level": "info", "message": "hi"}})

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("sdk_version", "1.2.0"); err != nil {
		t.Fatalf("write field: %v", err)
	}
	part, _ := w.CreateFormFile("file", "logs.tar.gz")
	part.Write(tarGz(t, map[string][]byte{"data.json": logs}))
	w.Close()

	rec, _ := serve(t, h, &body, w.FormDataContentType())

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if svc.logCount != 1 {
		t.Errorf("service saw %d logs, want 1", svc.logCount)
	}
}

func TestAnUploadWithNoFileFieldIsRejected(t *testing.T) {
	svc := &stubLogManager{}
	h := newLogHandler(t, svc)

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	w.WriteField("not_the_file", "nope")
	w.Close()

	rec, _ := serve(t, h, &body, w.FormDataContentType())

	if rec.Code == http.StatusOK {
		t.Fatal("an upload with no file field was accepted")
	}
	if svc.called {
		t.Error("the service ran on an upload with no file")
	}
}

func TestANonMultipartBodyIsRejected(t *testing.T) {
	svc := &stubLogManager{}
	h := newLogHandler(t, svc)

	body := bytes.NewBufferString(`{"logs":[]}`)
	rec, _ := serve(t, h, body, "application/json")

	if rec.Code == http.StatusOK {
		t.Fatal("a JSON body was accepted as an upload")
	}
	if svc.called {
		t.Error("the service ran on a non-multipart body")
	}
}

// Not a tar, not a gzip — just bytes. This must be a client error, not a 500.
func TestGarbageInsideTheFileFieldIsAClientError(t *testing.T) {
	svc := &stubLogManager{}
	h := newLogHandler(t, svc)

	body, contentType := multipartBody(t, "file", []byte("this is not a gzip stream"))
	rec, _ := serve(t, h, body, contentType)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// readErrorWatcher is what carries a cap error past gzip, tar and a JSON
// decoder, each of which is free to wrap or replace it.
func TestReadErrorWatcherKeepsTheFirstRealError(t *testing.T) {
	first := &http.MaxBytesError{Limit: 10}
	w := &readErrorWatcher{r: &erroringReader{errs: []error{nil, first, io.ErrUnexpectedEOF}}}

	buf := make([]byte, 4)
	w.Read(buf)
	w.Read(buf)
	w.Read(buf)

	if w.err != error(first) {
		t.Errorf("kept %v, want the first real error", w.err)
	}
}

// EOF is how a read ends normally, so it must never be mistaken for a failure.
func TestReadErrorWatcherIgnoresEOF(t *testing.T) {
	w := &readErrorWatcher{r: &erroringReader{errs: []error{io.EOF}}}
	buf := make([]byte, 4)
	w.Read(buf)

	if w.err != nil {
		t.Errorf("EOF was recorded as an error: %v", w.err)
	}
}

type erroringReader struct {
	errs []error
	at   int
}

func (e *erroringReader) Read(p []byte) (int, error) {
	if e.at >= len(e.errs) {
		return 0, io.EOF
	}
	err := e.errs[e.at]
	e.at++
	return 1, err
}
