package oalog

import (
	"fmt"

	"net/http"
	"runtime/debug"

	"time"

	"github.com/google/uuid"
)

type RequestID string
type LoggedInUser string

type RequestLog struct {
	RequestID    RequestID
	LoggedInUser LoggedInUser
	setLine      bool
}

type RequestCtrl interface {
	GetRequestID() RequestID
	GetLoggedInUser() LoggedInUser
}

func NewRequestLog(reqctrl RequestCtrl) *RequestLog {
	rl := RequestLog{}
	rl.RequestID = reqctrl.GetRequestID()
	rl.LoggedInUser = reqctrl.GetLoggedInUser()
	rl.setLine = true
	return &rl
}

func NewRequestCtrl(r *http.Request) *RequestLog {
	rl := RequestLog{}
	rl.RequestID = GetRequestID(r)
	rl.LoggedInUser = GetLoggedInUser(r)
	rl.setLine = true
	return &rl
}

func GetRequestID(r *http.Request) RequestID {
	return RequestID(r.Header.Get("request_id"))
}
func GetLoggedInUser(r *http.Request) LoggedInUser {
	return LoggedInUser(r.Header.Get("logged_user"))
}

func (rl RequestLog) GetRequestID() RequestID {
	return rl.RequestID
}

func (rl RequestLog) GetLoggedInUser() LoggedInUser {
	return rl.LoggedInUser
}

func (rl *RequestLog) SetLine(b bool) {
	rl.setLine = b
}

func (rl *RequestLog) Info(args ...interface{}) {
	if rl.setLine {
		args = append([]interface{}{getLine(), rl.RequestID + " ", rl.LoggedInUser + " "}, args...)
	}
	oalog.logger.Info(args...)
}

func (rl *RequestLog) Error(args ...interface{}) {
	if rl.setLine {
		args = append([]interface{}{getLine(), rl.RequestID + " ", rl.LoggedInUser + " "}, args...)
	}
	oalog.logger.Error(args...)
}

func (rl *RequestLog) Warn(args ...interface{}) {
	if rl.setLine {
		args = append([]interface{}{getLine(), rl.RequestID + " ", rl.LoggedInUser + " "}, args...)
	}
	oalog.logger.Warn(args...)
}

func (rl *RequestLog) Debug(args ...interface{}) {
	if rl.setLine {
		args = append([]interface{}{getLine(), rl.RequestID + " ", rl.LoggedInUser + " "}, args...)
	}
	oalog.logger.Debug(args...)
}

func (rl *RequestLog) Errorf(str string, args ...interface{}) {
	oalog.logger.Errorf(fmt.Sprint(getLine(), rl.RequestID+" ", rl.LoggedInUser+" ", str), args...)
}

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	responseData []byte
}

func NewLoggingResponseWriter(w http.ResponseWriter) *loggingResponseWriter {
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

var HttpLogger = func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := "req-" + uuid.New().String()
		logUser := ""
		user := r.Context().Value("User")
		if user != nil {
			logUser = "user(ID)=" + fmt.Sprint(user)
		}
		Info(fmt.Sprintf("Request %s-start %s: %s %s", reqID, logUser, r.Method, r.URL.Path))
		r.Header.Add("request_id", reqID)
		r.Header.Add("logged_user", logUser)
		role := r.Context().Value("Role")
		r.Header.Add("logged_role", fmt.Sprint(role))
		lrw := NewLoggingResponseWriter(w)
		next.ServeHTTP(lrw, r)
		responseBody := ""
		if lrw.statusCode >= 400 {
			responseBody = " Response body: " + string(lrw.responseData) + "."
		}
		Info(fmt.Sprintf("Completed request %s-end %s: %s %s %v (%v) in %v.%s", reqID, logUser, r.Method, r.URL.Path,
			http.StatusText(lrw.statusCode), lrw.statusCode, time.Since(start), responseBody))

	})
}

func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				if w.Header().Get("Content-Type") == "" {
					w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				}
				w.WriteHeader(http.StatusInternalServerError)
				f := "PANIC: "
				stack := fmt.Sprint(f, err, ", stack trace:", '\n')
				Errorf(stack)
				fmt.Println(string(debug.Stack()))
				stack = fmt.Sprint(stack, string(debug.Stack()))
				w.Write([]byte(stack))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
