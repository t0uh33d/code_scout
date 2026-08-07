package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/sirupsen/logrus"

	"github.com/getcodescout/code_scout/pkg/cslog"
)

// Recovery turns a panic into a 500 and a log line.
//
// The client is told nothing beyond the status. It used to be sent the panic
// value and the full Go stack trace, which hands anyone who can trigger a panic
// the source paths, package layout and function names of the server. The stack
// belongs in the log, where the operator is.
//
// It also printed the stack to stdout with fmt.Println, outside the logger
// entirely: no level, no timestamp, no request id, and nothing in the log file
// once one is configured.
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			err := recover()
			if err == nil {
				return
			}

			// This middleware is outermost, so the context here is the one from
			// before HttpLogger replaced it and carries no logger. The request
			// id survives on the header, which is shared with the copy, so the
			// panic can still be tied to the request that caused it.
			fields := logrus.Fields{
				"method": r.Method,
				"path":   r.URL.Path,
				"stack":  string(debug.Stack()),
			}
			if id := cslog.GetRequestID(r); id != "" {
				fields["request_id"] = string(id)
			}
			cslog.GetLogger().WithFields(fields).Errorf("PANIC: %v", err)

			// If a handler already started writing, the status is long gone and
			// WriteHeader here would only log a superfluous-call warning. There
			// is nothing better available: the response is already partly out.
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Internal server error\n"))
		}()
		next.ServeHTTP(w, r)
	})
}
