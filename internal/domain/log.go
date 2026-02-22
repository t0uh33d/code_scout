package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CallPhase string

const (
	CallPhaseRequest  CallPhase = "request"
	CallPhaseResponse CallPhase = "response"
	CallPhaseError    CallPhase = "error"
)

type Log struct {
	ID            uuid.UUID
	SessionID     uuid.UUID
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
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     gorm.DeletedAt
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
