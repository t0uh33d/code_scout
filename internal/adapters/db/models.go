package db

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/pkg/utils"
	"gorm.io/gorm"
)

type ProjectModel struct {
	utils.GormBase
	Name        string `gorm:"type:varchar(255);not null"`
	Description string `gorm:"type:text;not null"`
}

func (ProjectModel) TableName() string {
	return "projects"
}

type ProjectSecretModel struct {
	utils.GormBase
	ProjectID string       `gorm:"type:char(36);not null;index"`
	SecretKey string       `gorm:"type:varchar(255);not null;uniqueIndex"`
	Project   ProjectModel `gorm:"foreignKey:ProjectID;references:ID"`
}

func (ProjectSecretModel) TableName() string {
	return "project_secrets"
}

type LogModel struct {
	utils.GormBase
	SessionID     uuid.UUID         `gorm:"type:char(36);not null;index"`
	Level         string            `gorm:"type:varchar(50);not null"`
	Message       string            `gorm:"type:text"`
	Error         *string           `gorm:"type:text"`
	StackTrace    *json.RawMessage  `gorm:"type:text"`
	Metadata      *json.RawMessage  `gorm:"type:text"`
	Tags          *json.RawMessage  `gorm:"type:text"`
	TimeStamp     time.Time         `gorm:"type:datetime;not null;default:CURRENT_TIMESTAMP"`
	IsNetworkCall bool              `gorm:"type:bool;not null;default:false"`
	RequestID     *uuid.UUID        `gorm:"type:char(36);index"`
	CallPhase     *domain.CallPhase `gorm:"type:varchar(50);not null;default:'request';check:call_phase IN ('request', 'response')"`
}

func (LogModel) TableName() string {
	return "logs"
}

type UserModel struct {
	utils.GormBase
	Username     string `gorm:"type:varchar(255);not null;uniqueIndex"`
	PasswordHash string `gorm:"type:varchar(255);not null"`
}

func (UserModel) TableName() string {
	return "users"
}

type UserSessionModel struct {
	utils.GormBase
	UserID    string    `gorm:"type:char(36);not null;index"`
	Token     string    `gorm:"type:char(36);not null;uniqueIndex"`
	ExpiresAt time.Time `gorm:"type:datetime;not null"`
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
