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

// ProjectFavoriteModel is per user, not per project: two people looking at the
// same instance keep separate favourites.
type ProjectFavoriteModel struct {
	GormBase
	UserID    uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_favorite_user_project,priority:1"`
	ProjectID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_favorite_user_project,priority:2"`
}

func (ProjectFavoriteModel) TableName() string {
	return "project_favorites"
}

// LogModel carries three composite indexes matching the query shapes the
// dashboard actually issues: the log list (project + time), the session
// timeline, and network call grouping. Single-column indexes cannot serve
// these — the list query filesorts every row for the project without one.
type LogModel struct {
	GormBase

	// Unique per project so a retried upload is a no-op. Scoped to the project
	// rather than globally unique, so one project's clients can never suppress
	// another's rows by colliding on an id. Null for clients that send none,
	// and Postgres allows many nulls in a unique index.
	ClientID *uuid.UUID `gorm:"type:uuid;uniqueIndex:idx_logs_client,priority:2"`

	ProjectID     uuid.UUID         `gorm:"type:uuid;not null;index:idx_logs_list,priority:1;index:idx_logs_session,priority:1;index:idx_logs_request,priority:1;index:idx_logs_net,priority:1;uniqueIndex:idx_logs_client,priority:1"`
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
	Name string `gorm:"type:varchar(255);not null"`
	// Stored lower-cased by the service so the unique index is case-insensitive
	// without needing the citext extension.
	Email        string `gorm:"type:varchar(255);not null;uniqueIndex"`
	PasswordHash string `gorm:"type:varchar(255);not null"`
	// Role is checked on every request, so it is indexed: counting super admins
	// is on the hot path of every removal and demotion.
	Role string `gorm:"type:varchar(20);not null;default:'member';index;check:role IN ('super_admin', 'admin', 'member')"`
	// Set for generated temporary passwords, cleared once the person picks
	// their own.
	MustChangePassword bool `gorm:"type:boolean;not null;default:false"`
}

func (UserModel) TableName() string {
	return "users"
}

// ProjectMemberModel is the join that decides visibility. Everyone except a
// super admin sees exactly the projects they have a row here for, which keeps
// the rule to one join rather than a role branch in every query.
type ProjectMemberModel struct {
	GormBase
	UserID    uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_project_member,priority:1"`
	ProjectID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_project_member,priority:2;index"`
	Level     string    `gorm:"type:varchar(20);not null;default:'viewer';check:level IN ('viewer', 'maintainer')"`
}

func (ProjectMemberModel) TableName() string {
	return "project_members"
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
