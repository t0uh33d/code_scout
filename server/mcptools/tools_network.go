package mcptools

import (
	"context"
	"errors"
	"time"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type listNetworkCallsIn struct {
	ProjectID string `json:"project_id" jsonschema:"The project's UUID."`
	Path      string `json:"path,omitempty" jsonschema:"Substring match on the URL path."`
	Method    string `json:"method,omitempty" jsonschema:"Exact HTTP method, e.g. GET or POST."`
	Status    string `json:"status,omitempty" jsonschema:"A class: 2xx, 3xx, 4xx, 5xx, or failed for calls that ended in a transport error."`
	SessionID string `json:"session_id,omitempty" jsonschema:"Narrow to one launch's calls."`
	Limit     int    `json:"limit,omitempty" jsonschema:"Most calls to return, newest first. Default 50, maximum 200."`
}

type networkCallItem struct {
	// RequestID correlates the call's phases; get_network_request takes it.
	RequestID  string  `json:"request_id"`
	SessionID  string  `json:"session_id"`
	Method     *string `json:"method,omitempty"`
	URL        *string `json:"url,omitempty"`
	StatusCode *int    `json:"status_code,omitempty"`
	// State is complete, error, pending (no answer yet or ever), or
	// response-only (an answer whose request phase never arrived).
	State        string    `json:"state"`
	StartedAt    time.Time `json:"started_at"`
	DurationMS   int64     `json:"duration_ms"`
	ErrorMessage *string   `json:"error_message,omitempty"`
}

type listNetworkCallsOut struct {
	Calls []networkCallItem `json:"calls"`
}

type getNetworkRequestIn struct {
	ProjectID string `json:"project_id" jsonschema:"The project's UUID."`
	RequestID string `json:"request_id" jsonschema:"The call's request id, from list_network_calls or a log row."`
}

type getNetworkRequestOut struct {
	// Phases are the call's raw logs, whole and in order: the request, then
	// its response or error. Headers and bodies live in each phase's
	// metadata, subject to whatever redaction the app configured.
	Phases []toolLog `json:"phases"`
}

var (
	errRequestNotFound = errNotFoundFor("network request")
	errBadStatusClass  = errors.New("status must be one of 2xx, 3xx, 4xx, 5xx or failed")
)

func validStatusClass(s string) bool {
	switch s {
	case "", "2xx", "3xx", "4xx", "5xx", "failed":
		return true
	}
	return false
}

func (d Deps) addNetworkTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_network_calls",
		Description: "A project's HTTP calls, one row per call with its phases already paired: " +
			"method, URL, status, duration and state. Filter by path, method, status class or session.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listNetworkCallsIn) (*mcp.CallToolResult, listNetworkCallsOut, error) {
		var out listNetworkCallsOut
		projectID, err := d.requireProject(ctx, in.ProjectID)
		if err != nil {
			return nil, out, err
		}
		// Refused rather than ignored: a typo silently matching everything
		// would read as "no failing calls", which is a lie.
		if !validStatusClass(in.Status) {
			return nil, out, errBadStatusClass
		}

		filter := domain.NetworkFilter{Path: in.Path, Method: in.Method, Status: in.Status}
		if in.SessionID != "" {
			sid, err := uuid.Parse(in.SessionID)
			if err != nil {
				return nil, out, errSessionNotFound
			}
			filter.SessionID = &sid
		}

		calls, err := d.Logs.ListNetworkCalls(ctx, projectID, filter, clampLimit(in.Limit, 50, 200))
		if err != nil {
			return nil, out, internal(ctx, err)
		}
		out.Calls = make([]networkCallItem, 0, len(calls))
		for _, c := range calls {
			item := networkCallItem{
				RequestID:    c.RequestID.String(),
				SessionID:    c.SessionID.String(),
				Method:       c.Method,
				URL:          c.URL,
				StatusCode:   c.StatusCode,
				State:        string(c.State()),
				StartedAt:    c.StartedAt,
				DurationMS:   c.Duration().Milliseconds(),
				ErrorMessage: c.ErrorMessage,
			}
			out.Calls = append(out.Calls, item)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "get_network_request",
		Description: "One HTTP call's raw phase logs, whole: the request and its response or " +
			"error, with full headers and bodies in each phase's metadata.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getNetworkRequestIn) (*mcp.CallToolResult, getNetworkRequestOut, error) {
		var out getNetworkRequestOut
		projectID, err := d.requireProject(ctx, in.ProjectID)
		if err != nil {
			return nil, out, err
		}
		requestID, err := uuid.Parse(in.RequestID)
		if err != nil {
			return nil, out, errRequestNotFound
		}
		phases, err := d.Logs.GetNetworkRequest(ctx, projectID, requestID)
		if err != nil {
			return nil, out, internal(ctx, err)
		}
		if len(phases) == 0 {
			return nil, out, errRequestNotFound
		}
		out.Phases = make([]toolLog, 0, len(phases))
		for _, l := range phases {
			out.Phases = append(out.Phases, wholeLog(l))
		}
		return nil, out, nil
	})
}
