package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/getcodescout/code_scout/pkg/cslog"
)

// run drives one request through the middleware and hands back the JSON lines
// it produced. JSON rather than text so an assertion can be about a field's
// type, which is the point of half of these.
func run(t *testing.T, level logrus.Level, req *http.Request, h http.HandlerFunc) []map[string]any {
	t.Helper()

	logger := cslog.GetLogger()
	oldOut, oldLevel, oldFmt := logger.Out, logger.Level, logger.Formatter
	t.Cleanup(func() { logger.SetOutput(oldOut); logger.SetLevel(oldLevel); logger.SetFormatter(oldFmt) })

	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.SetLevel(level)
	logger.SetFormatter(&logrus.JSONFormatter{})

	HttpLogger(h).ServeHTTP(httptest.NewRecorder(), req)

	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if raw == "" {
			continue
		}
		var line map[string]any
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatalf("log line is not json: %v\n%s", err, raw)
		}
		lines = append(lines, line)
	}
	return lines
}

func ok(w http.ResponseWriter, r *http.Request) { w.Write([]byte("hello")) }

// One line per request at info, not two. The start line said nothing the
// finished line does not, and doubled the volume of every log.
func TestOneLinePerRequestAtInfo(t *testing.T) {
	lines := run(t, logrus.InfoLevel, httptest.NewRequest("GET", "/project/x/logs", nil), ok)

	if len(lines) != 1 {
		t.Fatalf("got %d lines at info, want 1: %v", len(lines), lines)
	}
	line := lines[0]

	if line["method"] != "GET" || line["path"] != "/project/x/logs" {
		t.Errorf("method/path missing: %v", line)
	}
	if line["status"] != float64(http.StatusOK) {
		t.Errorf("status = %v, want 200", line["status"])
	}
	if line["bytes"] != float64(len("hello")) {
		t.Errorf("bytes = %v, want 5", line["bytes"])
	}
	if id, _ := line["request_id"].(string); !strings.HasPrefix(id, "req-") {
		t.Errorf("request_id = %v", line["request_id"])
	}
}

// duration_ms has to be a number. As time.Duration's String() it was "1.4ms"
// and "1.4s" in the same field, which cannot be sorted or compared, so "show me
// everything slower than 500ms" was not a question the log could answer.
func TestDurationIsANumberOfMilliseconds(t *testing.T) {
	lines := run(t, logrus.InfoLevel, httptest.NewRequest("GET", "/project/x/logs", nil), ok)

	got, isNumber := lines[0]["duration_ms"].(float64)
	if !isNumber {
		t.Fatalf("duration_ms is %T (%v), want a number", lines[0]["duration_ms"], lines[0]["duration_ms"])
	}
	if got < 0 {
		t.Errorf("duration_ms = %v", got)
	}
}

// A health probe every 15 seconds and every static asset on every page load is
// most of the volume and none of the information.
func TestProbesAndAssetsAreQuietAtInfo(t *testing.T) {
	for _, path := range []string{"/healthz", "/static/css/tailwind.css"} {
		t.Run(path, func(t *testing.T) {
			if lines := run(t, logrus.InfoLevel, httptest.NewRequest("GET", path, nil), ok); len(lines) != 0 {
				t.Errorf("%s logged %d lines at info, want 0: %v", path, len(lines), lines)
			}
			// Still there when you go looking.
			if lines := run(t, logrus.DebugLevel, httptest.NewRequest("GET", path, nil), ok); len(lines) == 0 {
				t.Errorf("%s logged nothing even at debug", path)
			}
		})
	}
}

// A 500 is the one status worth waking up for, so it is logged as an error
// rather than buried among the successful requests.
func TestServerErrorsLogAtErrorLevel(t *testing.T) {
	boom := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) }
	lines := run(t, logrus.InfoLevel, httptest.NewRequest("GET", "/project/x/logs", nil), boom)

	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if lines[0]["level"] != "error" {
		t.Errorf("a 500 logged at %v, want error", lines[0]["level"])
	}
}

// The response body used to be logged on any 4xx or 5xx, which put whatever the
// handler had written into the log. It was also only ever the last chunk, so it
// was a fragment as well as a leak.
func TestTheResponseBodyIsNotLogged(t *testing.T) {
	secret := "sk_live_do_not_log_this"
	leaky := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("failed for " + secret))
	}
	lines := run(t, logrus.DebugLevel, httptest.NewRequest("GET", "/project/x/logs", nil), leaky)

	for _, line := range lines {
		if raw, _ := json.Marshal(line); strings.Contains(string(raw), secret) {
			t.Errorf("the response body reached the log: %s", raw)
		}
	}
}

// Behind nginx every request arrives from 127.0.0.1, so the forwarded address
// is the only one worth recording. It is for reading, never for a decision:
// a client talking to the server directly can set it to anything.
func TestRemoteIPPrefersTheForwardedAddress(t *testing.T) {
	req := httptest.NewRequest("GET", "/project/x/logs", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")

	if got := run(t, logrus.InfoLevel, req, ok)[0]["remote_ip"]; got != "203.0.113.7" {
		t.Errorf("remote_ip = %v, want the first forwarded address", got)
	}

	bare := httptest.NewRequest("GET", "/project/x/logs", nil)
	bare.RemoteAddr = "198.51.100.4:1234"
	if got := run(t, logrus.InfoLevel, bare, ok)[0]["remote_ip"]; got != "198.51.100.4" {
		t.Errorf("remote_ip = %v, want the port stripped", got)
	}
}

// A panic used to send the client the panic value and the full Go stack trace,
// which hands anyone who can trigger one the source paths, package layout and
// function names of the server. The stack belongs in the log.
func TestPanicTellsTheClientNothingAndTheLogEverything(t *testing.T) {
	logger := cslog.GetLogger()
	oldOut, oldLevel, oldFmt := logger.Out, logger.Level, logger.Formatter
	t.Cleanup(func() { logger.SetOutput(oldOut); logger.SetLevel(oldLevel); logger.SetFormatter(oldFmt) })

	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.SetLevel(logrus.InfoLevel)
	logger.SetFormatter(&logrus.JSONFormatter{})

	// A panic value shaped like a format string. It was passed to Errorf as the
	// format itself, so this used to come out mangled.
	boom := func(w http.ResponseWriter, r *http.Request) { panic("bad %s thing %d") }

	rec := httptest.NewRecorder()
	Recovery(http.HandlerFunc(boom)).ServeHTTP(rec, httptest.NewRequest("GET", "/project/x/logs", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	body := rec.Body.String()
	for _, leak := range []string{"goroutine", "runtime.", ".go:", "middleware."} {
		if strings.Contains(body, leak) {
			t.Errorf("the response leaks internals (%q): %s", leak, body)
		}
	}

	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &line); err != nil {
		t.Fatalf("the panic was not logged as json: %v\n%s", err, buf.String())
	}
	if line["level"] != "error" {
		t.Errorf("panic logged at %v, want error", line["level"])
	}
	if msg, _ := line["msg"].(string); !strings.Contains(msg, "bad %s thing %d") {
		t.Errorf("the panic value was mangled by format expansion: %q", msg)
	}
	if stack, _ := line["stack"].(string); !strings.Contains(stack, "goroutine") {
		t.Error("the stack trace did not reach the log")
	}
}
