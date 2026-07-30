package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type CallPhase string

const (
	CallPhaseRequest  CallPhase = "request"
	CallPhaseResponse CallPhase = "response"
	CallPhaseError    CallPhase = "error"
)

type Log struct {
	ID uuid.UUID

	// ClientID is the identifier the SDK generated on the device. It makes
	// ingestion idempotent: if an upload succeeds but the response is lost, the
	// SDK retries the same batch and the re-sent rows are recognised and
	// skipped instead of inserted twice.
	ClientID *uuid.UUID

	ProjectID uuid.UUID
	SessionID uuid.UUID
	Level         string
	Message       string
	Error         *string
	StackTrace    *json.RawMessage
	Metadata      *json.RawMessage
	Tags          *json.RawMessage
	TimeStamp     time.Time
	IsNetworkCall bool
	RequestID     *uuid.UUID
	CallPhase     *CallPhase

	// Extracted from Metadata at ingest so the network list can filter and sort
	// on indexed columns. A JSON_EXTRACT predicate cannot use an index, which
	// would make every method or status filter a table scan.
	Method     *string
	URL        *string
	StatusCode *int

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

// NetworkMeta is the subset of a network log's metadata that gets promoted to
// real columns. The SDK puts method and url at the top level on the request
// phase, and nests them under "request" on the response and error phases.
type NetworkMeta struct {
	Method     *string `json:"method"`
	URL        *string `json:"url"`
	StatusCode *int    `json:"status_code"`
	Request    *struct {
		Method *string `json:"method"`
		URL    *string `json:"url"`
	} `json:"request"`
}

// ExtractNetworkMeta pulls method, url and status code out of a network log's
// metadata. Returns nils for non-network logs or unparseable metadata — a bad
// payload must never fail ingestion.
func ExtractNetworkMeta(raw *json.RawMessage) (method, url *string, statusCode *int) {
	if raw == nil || len(*raw) == 0 {
		return nil, nil, nil
	}
	var m NetworkMeta
	if err := json.Unmarshal(*raw, &m); err != nil {
		return nil, nil, nil
	}

	method, url = m.Method, m.URL
	if m.Request != nil {
		if method == nil {
			method = m.Request.Method
		}
		if url == nil {
			url = m.Request.URL
		}
	}
	return method, url, m.StatusCode
}

// CustomBool handles int to bool conversion
type CustomBool bool

// UnmarshalJSON implements custom unmarshalling for CustomBool
func (cb *CustomBool) UnmarshalJSON(data []byte) error {
	var intValue int
	if err := json.Unmarshal(data, &intValue); err != nil {
		return err
	}
	*cb = intValue != 0
	return nil
}

type IncomingLog struct {
	ID            uuid.UUID        `json:"id"`
	SessionID     uuid.UUID        `json:"session_id"`
	Level         string           `json:"level"`
	Message       string           `json:"message"`
	Error         *string          `json:"error"`
	StackTrace    *json.RawMessage `json:"stack_trace"`
	Metadata      *json.RawMessage `json:"metadata"`
	Tags          *json.RawMessage `json:"tags"`
	Timestamp     time.Time        `json:"timestamp"`
	IsNetworkCall CustomBool       `json:"is_network_call"`
	RequestID     *uuid.UUID       `json:"request_id"`
	CallPhase     *CallPhase       `json:"call_phase"`
}
