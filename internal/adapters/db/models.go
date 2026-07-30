package db

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
	"gorm.io/gorm"
)

type ProjectModel struct {
	GormBase
	Name        string `gorm:"type:varchar(255);not null"`
	Description string `gorm:"type:text;not null"`
}

func (ProjectModel) TableName() string {
	return "projects"
}

type ProjectSecretModel struct {
	GormBase
	ProjectID string       `gorm:"type:uuid;not null;index"`
	SecretKey string       `gorm:"type:varchar(255);not null;uniqueIndex"`
	Project   ProjectModel `gorm:"foreignKey:ProjectID;references:ID"`
}

func (ProjectSecretModel) TableName() string {
	return "project_secrets"
}

// LogModel carries three composite indexes matching the query shapes the
// dashboard actually issues: the log list (project + time), the session
// timeline, and network call grouping. Single-column indexes cannot serve
// these — the list query filesorts every row for the project without one.
type LogModel struct {
	GormBase
	ProjectID     uuid.UUID         `gorm:"type:uuid;not null;index:idx_logs_list,priority:1;index:idx_logs_session,priority:1;index:idx_logs_request,priority:1;index:idx_logs_net,priority:1"`
	SessionID     uuid.UUID         `gorm:"type:uuid;not null;index:idx_logs_session,priority:2"`
	Project       ProjectModel      `gorm:"foreignKey:ProjectID;references:ID"`
	Level         string            `gorm:"type:varchar(50);not null"`
	Message       string            `gorm:"type:text"`
	Error         *string           `gorm:"type:text"`
	StackTrace    *json.RawMessage  `gorm:"type:jsonb"`
	Metadata      *json.RawMessage  `gorm:"type:jsonb"`
	Tags          *json.RawMessage  `gorm:"type:jsonb;index:,type:gin"`
	TimeStamp     time.Time         `gorm:"type:timestamptz;not null;default:now();index:idx_logs_list,priority:2,sort:desc;index:idx_logs_session,priority:3"`
	IsNetworkCall bool              `gorm:"type:boolean;not null;default:false"`
	RequestID     *uuid.UUID        `gorm:"type:uuid;index:idx_logs_request,priority:2"`
	CallPhase     *domain.CallPhase `gorm:"type:varchar(50);check:call_phase IN ('request', 'response', 'error')"`

	// Promoted out of Metadata at ingest — see domain.ExtractNetworkMeta.
	// Sorting and range-filtering these is far cheaper on real columns than on
	// jsonb fields, and it keeps the network list query readable.
	Method     *string `gorm:"type:varchar(10);index:idx_logs_net,priority:2"`
	URL        *string `gorm:"type:varchar(2048)"`
	StatusCode *int    `gorm:"type:int;index:idx_logs_net,priority:3"`
}

func (LogModel) TableName() string {
	return "logs"
}

type UserModel struct {
	GormBase
	Username     string `gorm:"type:varchar(255);not null;uniqueIndex"`
	PasswordHash string `gorm:"type:varchar(255);not null"`
}

func (UserModel) TableName() string {
	return "users"
}

type UserSessionModel struct {
	GormBase
	UserID    string    `gorm:"type:uuid;not null;index"`
	Token     string    `gorm:"type:uuid;not null;uniqueIndex"`
	ExpiresAt time.Time `gorm:"type:timestamptz;not null"`
	User      UserModel `gorm:"foreignKey:UserID;references:ID"`
}

func (UserSessionModel) TableName() string {
	return "user_sessions"
}

func (m *ProjectModel) Create(tx *gorm.DB) error {
	return tx.Create(m).Error
}

func (m *ProjectModel) Delete(tx *gorm.DB) error {
	return tx.Delete(m).Error
}

func (m *ProjectSecretModel) Create(tx *gorm.DB) error {
	return tx.Create(m).Error
}

func (m *ProjectSecretModel) Delete(tx *gorm.DB) error {
	return tx.Delete(m).Error
}

func (m *LogModel) Create(tx *gorm.DB) error {
	return tx.Create(m).Error
}
