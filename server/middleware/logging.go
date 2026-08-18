package middleware

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/getcodescout/code_scout/pkg/cslog"
)

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	bytes      int
}

func newLoggingResponseWriter(w http.ResponseWriter) *loggingResponseWriter {
	return &loggingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

// The body is counted, not kept.
//
// It used to be retained and logged on any 4xx or 5xx, which put whatever a
// handler had written into the log: an error page, a form's contents, anything
// a future handler decides to echo back. It was also wrong — this assignment
// overwrote on every call, so what got logged was the last chunk rather than
// the body. A size is the part that was ever useful.
func (lrw *loggingResponseWriter) Write(b []byte) (int, error) {
	size, err := lrw.ResponseWriter.Write(b)
	lrw.bytes += size
	return size, err
}

// Hijack hands the underlying connection over, which is what a WebSocket
// upgrade needs.
//
// Wrapping a ResponseWriter in a struct hides every interface the real one
// implements beyond ResponseWriter itself, because a type assertion sees the
// wrapper. Without this, the live device socket answered
// "response does not implement http.Hijacker" and no device could ever
// connect — the logging middleware, of all things, made streaming impossible.
func (lrw *loggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := lrw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("the underlying ResponseWriter does not support hijacking")
	}
	return hijacker.Hijack()
}

// Flush passes a flush through for the same reason. A streaming response that
// cannot flush is buffered until it ends, which for SSE means never.
func (lrw *loggingResponseWriter) Flush() {
	if flusher, ok := lrw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap is how http.ResponseController reaches the real ResponseWriter, and
// through it SetWriteDeadline.
//
// This is the third interface the wrapper has had to hand back, after Hijacker
// and Flusher, and the only one that failed quietly. A ResponseController that
// cannot find the writer does not panic and does not log: it returns
// "feature not supported", which the SSE handlers pass to a Warn nobody reads.
// The stream then keeps the write deadline the server set for an ordinary
// request, so every live tail and every live session watcher was cut off after
// WriteTimeout — thirty seconds, on connections meant to stay open all
// afternoon. TestSSECanClearItsWriteDeadline fails if this is removed.
func (lrw *loggingResponseWriter) Unwrap() http.ResponseWriter {
	return lrw.ResponseWriter
}

// routine reports whether a path is polled rather than visited. A health probe
// every 15 seconds and every static asset on every page load are most of the
// volume and none of the information, so they log at debug and the interesting
// requests stay readable at info.
func routine(path string) bool {
	return path == "/healthz" || strings.HasPrefix(path, "/static/")
}

// clientIP prefers the address a proxy reports, since every request behind
// nginx otherwise arrives from 127.0.0.1.
//
// X-Forwarded-For is set by whoever is in front and can be forged by a client
// talking to the server directly, so this is for reading logs, never for a
// decision about access.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if comma := strings.IndexByte(fwd, ','); comma > 0 {
			return strings.TrimSpace(fwd[:comma])
		}
		return strings.TrimSpace(fwd)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// HttpLogger creates a request-scoped logger with request_id and user_id fields
// and stores it in context so any downstream layer can call cslog.L(ctx).
//
// One line per request, written when it finishes. There used to be a second
// line at the start carrying nothing the finish line does not, which doubled
// the volume for no information. It survives at debug, where it earns its keep
// on the one case the finish line cannot cover: a request that hangs and never
// completes leaves a start with no end.
func HttpLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		reqID := "req-" + uuid.New().String()

		logUser := ""
		if user := r.Context().Value("User"); user != nil {
			logUser = fmt.Sprint(user)
		}

		// Build a child logger that carries request_id + user context on every line.
		fields := logrus.Fields{
			"request_id": reqID,
			"method":     r.Method,
			"path":       r.URL.Path,
		}
		if logUser != "" {
			fields["user_id"] = logUser
		}
		log := cslog.GetLogger().WithFields(fields)

		// Store in context so services/adapters can call cslog.L(ctx).
		ctx := cslog.WithLogger(r.Context(), log)
		r = r.WithContext(ctx)

		log.Debug("Request start")

		r.Header.Add("request_id", reqID)
		r.Header.Add("logged_user", logUser)

		lrw := newLoggingResponseWriter(w)
		next.ServeHTTP(lrw, r)

		// duration_ms as a number, not time.Duration's String(). That renders
		// "1.4ms" and "1.4s", which neither sort nor aggregate, so "show me
		// everything over 500ms" was not a question the log could answer.
		done := log.WithFields(logrus.Fields{
			"status":      lrw.statusCode,
			"duration_ms": float64(time.Since(start).Microseconds()) / 1000,
			"bytes":       lrw.bytes,
			"remote_ip":   clientIP(r),
		})

		switch {
		case lrw.statusCode >= 500:
			done.Error("Request failed")
		case routine(r.URL.Path):
			done.Debug("Request")
		default:
			done.Info("Request")
		}
	})
}
