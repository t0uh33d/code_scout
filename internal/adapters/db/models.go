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

// InstanceSettingsModel holds the single row of runtime configuration. The
// service keeps it to one row; nothing here enforces that, because a stray
// second row is a bug to fix rather than a constraint to carry.
type InstanceSettingsModel struct {
	GormBase
	Timezone string `gorm:"type:varchar(64);not null;default:'UTC'"`

	// Defaults mirror the domain's, so a row created before these columns
	// existed reads back as the behaviour that was hardcoded before them.
	RetentionDays  int   `gorm:"not null;default:30"`
	PurgeAfterDays int   `gorm:"not null;default:7"`
	MaxUploadBytes int64 `gorm:"not null;default:52428800"`
	DailyLogCap    int64 `gorm:"not null;default:0"`
}

func (InstanceSettingsModel) TableName() string {
	return "instance_settings"
}

// ProjectUsageDailyModel is one row per project per day. Both reads and writes
// are single-row lookups on the composite key, so the cost of enforcing a cap
// does not grow with the volume being capped.
type ProjectUsageDailyModel struct {
	ProjectID uuid.UUID `gorm:"type:uuid;primaryKey"`
	// A date, not a timestamp: the bucket is a UTC calendar day.
	Day       time.Time `gorm:"type:date;primaryKey"`
	LogCount  int64     `gorm:"not null;default:0"`
	UpdatedAt time.Time
}

func (ProjectUsageDailyModel) TableName() string {
	return "project_usage_daily"
}

// SessionModel is one app launch.
//
// The primary key is the client's own session id, not a generated one: the SDK
// re-sends this record with every batch, and keying on its id is what makes the
// repeat an update rather than a duplicate.
type SessionModel struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	ProjectID uuid.UUID `gorm:"type:uuid;not null;index:idx_sessions_project_started,priority:1"`

	InstallationID *uuid.UUID `gorm:"type:uuid;index:idx_sessions_installation"`
	// Indexed because "every session this user has been on" is a support
	// question people actually ask.
	UserID *string `gorm:"type:varchar(255);index:idx_sessions_user"`

	DeviceModel *string `gorm:"type:varchar(255)"`
	OSName      *string `gorm:"type:varchar(64)"`
	OSVersion   *string `gorm:"type:varchar(64)"`
	AppVersion  *string `gorm:"type:varchar(64)"`
	BuildNumber *string `gorm:"type:varchar(64)"`

	Metadata *json.RawMessage `gorm:"type:jsonb"`

	// Descending in the index because every list is newest first.
	StartedAt  time.Time `gorm:"not null;index:idx_sessions_project_started,priority:2,sort:desc"`
	LastSeenAt time.Time `gorm:"not null"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (SessionModel) TableName() string {
	return "sessions"
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

	// Indexed with the project because every error query is "this project's
	// groups", never a fingerprint across all of them.
	Fingerprint *string `gorm:"type:varchar(320);index:idx_logs_project_fingerprint,priority:2"`
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
