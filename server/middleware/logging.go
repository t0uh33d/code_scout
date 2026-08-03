package middleware

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/getcodescout/code_scout/pkg/cslog"
)

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	responseData []byte
}

func newLoggingResponseWriter(w http.ResponseWriter) *loggingResponseWriter {
	return &loggingResponseWriter{w, http.StatusOK, []byte{}}
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (lrw *loggingResponseWriter) Write(b []byte) (int, error) {
	size, err := lrw.ResponseWriter.Write(b)
	lrw.responseData = b
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

// HttpLogger creates a request-scoped logger with request_id and user_id fields
// and stores it in context so any downstream layer can call cslog.L(ctx).
func HttpLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		reqID := "req-" + uuid.New().String()

		logUser := ""
		user := r.Context().Value("User")
		if user != nil {
			logUser = "user(ID)=" + fmt.Sprint(user)
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

		log.Info("Request Start")

		r.Header.Add("request_id", reqID)
		r.Header.Add("logged_user", logUser)

		lrw := newLoggingResponseWriter(w)
		next.ServeHTTP(lrw, r)

		endFields := logrus.Fields{
			"status":   lrw.statusCode,
			"duration": time.Since(start).String(),
		}

		if lrw.statusCode >= 400 {
			endFields["response_body"] = string(lrw.responseData)
		}

		log.WithFields(endFields).Info("Request Complete")
	})
}
