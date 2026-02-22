package cslog

import (
	"context"
	"net/http"

	"github.com/sirupsen/logrus"
)

type ctxLoggerKey struct{}

// WithLogger returns a new context that carries the given logrus.Entry.
func WithLogger(ctx context.Context, logger *logrus.Entry) context.Context {
	return context.WithValue(ctx, ctxLoggerKey{}, logger)
}

// L extracts the logrus.Entry from the context.
// Falls back to a default entry from GlobalLogger if none is present.
func L(ctx context.Context) *logrus.Entry {
	if l, ok := ctx.Value(ctxLoggerKey{}).(*logrus.Entry); ok {
		return l
	}
	return logrus.NewEntry(oalog.logger)
}

type RequestID string
type LoggedInUser string

type RequestContext context.Context

type RequestLog struct {
	*logrus.Entry

	RequestID    RequestID
	LoggedInUser LoggedInUser
	ctx          RequestContext
}

// RequestCtrl defines an interface that any request context should provide.
type RequestCtrl interface {
	GetRequestID() RequestID
	GetLoggedInUser() LoggedInUser
	GetContext() RequestContext
}

// BaseCtrl is an embeddable struct that implements RequestCtrl.
// Embed this in your controllers to avoid repeating the same fields and getters.
type BaseCtrl struct {
	requestID    RequestID
	loggedInUser LoggedInUser
	ctx          RequestContext
}

func NewBaseCtrl(rc RequestCtrl) BaseCtrl {
	return BaseCtrl{
		requestID:    rc.GetRequestID(),
		loggedInUser: rc.GetLoggedInUser(),
		ctx:          rc.GetContext(),
	}
}

func (b *BaseCtrl) GetRequestID() RequestID       { return b.requestID }
func (b *BaseCtrl) GetLoggedInUser() LoggedInUser  { return b.loggedInUser }
func (b *BaseCtrl) GetContext() RequestContext     { return b.ctx }

// NewRequestLog constructs a new RequestLog using an existing RequestCtrl.
func NewRequestLog(reqctrl RequestCtrl) *RequestLog {
	rl := RequestLog{}
	rl.RequestID = reqctrl.GetRequestID()
	rl.LoggedInUser = reqctrl.GetLoggedInUser()
	rl.ctx = reqctrl.GetContext()

	fields := logrus.Fields{}
	if rl.RequestID != "" {
		fields["request_id"] = string(rl.RequestID)
	}
	if rl.LoggedInUser != "" {
		fields["user_id"] = string(rl.LoggedInUser)
	}

	rl.Entry = oalog.logger.WithFields(fields)
	return &rl
}

// NewRequestCtrl creates a RequestLog from an incoming HTTP request.
func NewRequestCtrl(r *http.Request) *RequestLog {
	rl := RequestLog{}
	rl.RequestID = GetRequestID(r)
	rl.LoggedInUser = GetLoggedInUser(r)
	rl.ctx = r.Context()

	fields := logrus.Fields{}
	if rl.RequestID != "" {
		fields["request_id"] = string(rl.RequestID)
	}
	if rl.LoggedInUser != "" {
		fields["user_id"] = string(rl.LoggedInUser)
	}

	rl.Entry = oalog.logger.WithFields(fields)
	return &rl
}

func GetRequestID(r *http.Request) RequestID {
	return RequestID(r.Header.Get("request_id"))
}

func GetLoggedInUser(r *http.Request) LoggedInUser {
	return LoggedInUser(r.Header.Get("logged_user"))
}

func (rl RequestLog) GetRequestID() RequestID       { return rl.RequestID }
func (rl RequestLog) GetLoggedInUser() LoggedInUser  { return rl.LoggedInUser }
func (rl RequestLog) GetContext() RequestContext     { return rl.ctx }
