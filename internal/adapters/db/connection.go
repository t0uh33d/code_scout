package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/getcodescout/code_scout/pkg/cslog"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type DBConfig struct {
	User     string
	Password string
	Database string
	Host     string
	Port     int

	// SSLMode maps to libpq sslmode: "disable", "require", "verify-ca" or
	// "verify-full". Managed databases usually need at least "require".
	SSLMode string

	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// connectRetries is how many times to retry the initial connection. Containers
// routinely start before the database is accepting connections, and Aurora
// Serverless needs a moment to wake, so failing on the first dial is wrong.
const connectRetries = 10

func NewConnection(cfg DBConfig) (*gorm.DB, error) {
	if cfg.SSLMode == "" {
		cfg.SSLMode = "disable"
	}

	// The DSN contains the password, so it is never logged.
	// TimeZone=UTC keeps timestamps unambiguous regardless of server locale.
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=UTC",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Database, cfg.SSLMode,
	)

	cslog.Info(fmt.Sprintf("Connecting to Postgres at %s:%d/%s (sslmode=%s)", cfg.Host, cfg.Port, cfg.Database, cfg.SSLMode))

	var db *gorm.DB
	var err error
	backoff := 500 * time.Millisecond

	for attempt := 1; attempt <= connectRetries; attempt++ {
		// gorm.Open is lazy, so a successful open proves nothing. Ping is what
		// actually tells us the database is accepting connections.
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
		if err == nil {
			var sqlDB *sql.DB
			if sqlDB, err = db.DB(); err == nil {
				if err = sqlDB.Ping(); err == nil {
					break
				}
			}
		}

		if attempt == connectRetries {
			return nil, fmt.Errorf("could not reach database at %s:%d after %d attempts: %w",
				cfg.Host, cfg.Port, connectRetries, err)
		}

		cslog.Info(fmt.Sprintf("Waiting for database (attempt %d/%d): %v", attempt, connectRetries, err))
		time.Sleep(backoff)
		if backoff < 8*time.Second {
			backoff *= 2
		}
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to access underlying sql.DB: %w", err)
	}

	// Without limits GORM opens connections without bound, which exhausts the
	// connection cap on a small managed instance long before the app is busy.
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}

	return db, nil
}

// Ping reports whether the database is currently reachable. Used by /healthz.
func Ping(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}

func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&ProjectModel{},
		&ProjectSecretModel{},
		&ProjectFavoriteModel{},
		&ProjectMemberModel{},
		&LogModel{},
		&UserModel{},
		&UserSessionModel{},
		&InstanceSettingsModel{},
		&SessionModel{},
		&ProjectUsageDailyModel{},
		&PersonalAccessTokenModel{},
	)
}
