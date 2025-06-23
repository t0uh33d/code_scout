package db

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/utils"
	"gorm.io/gorm"
)

type CallPhase string

const (
	CallPhaseRequest  CallPhase = "request"
	CallPhaseResponse CallPhase = "response"
	CallPhaseError    CallPhase = "error"
)

type Logs struct {
	utils.GormBase

	SessionID     uuid.UUID        `gorm:"type:char(36);not null;index"`
	Level         string           `gorm:"type:varchar(50);not null"`
	Message       string           `gorm:"type:text"`
	Error         *string          `gorm:"type:text"`
	StackTrace    *json.RawMessage `gorm:"type:text"`
	Metadata      *json.RawMessage `gorm:"type:text"`
	Tags          *json.RawMessage `gorm:"type:text"`
	TimeStamp     time.Time        `gorm:"type:datetime;not null;default:CURRENT_TIMESTAMP"`
	IsNetworkCall bool             `gorm:"type:bool;not null;default:false"`
	RequestID     *uuid.UUID       `gorm:"type:char(36);index"`
	CallPhase     *CallPhase       `gorm:"type:varchar(50);not null;default:'request';check:call_phase IN ('request', 'response')"`
}

func (logs *Logs) Create(tx *gorm.DB) error {
	return tx.Create(logs).Error
}

func (logs *Logs) Update(tx *gorm.DB) error {
	return tx.Save(logs).Error
}

func (logs *Logs) Delete(tx *gorm.DB) error {
	return tx.Delete(logs).Error
}
