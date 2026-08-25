package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/getcodescout/code_scout/pkg/cslog"
	"github.com/sirupsen/logrus"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
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

// gormLogWriter sends GORM's own output through cslog, so it obeys log_file and
// log_level like everything else the server writes.
type gormLogWriter struct{}

func (gormLogWriter) Printf(format string, args ...any) {
	cslog.Debugf(strings.TrimSpace(format), args...)
}

// gormLogger replaces GORM's default, which cannot be used here.
//
// The default writes to os.Stdout through its own log.Logger, so it ignores
// log_file and log_level entirely. Worse, its default config is
// LogLevel: Warn with IgnoreRecordNotFoundError: false, and the guard in
// gorm/logger.Trace reduces under those settings to "any error at all". It then
// prints the *fully interpolated* SQL of the failing statement.
//
// Every request carrying an expired, revoked or simply stale cs_session does a
// lookup that misses, which is ErrRecordNotFound, which printed
// `SELECT * FROM user_sessions WHERE token = '<the live token>'` to stdout. The
// same held for personal access tokens on /api/mcp. A logged-out browser tab
// was enough to put a working credential in the journal.
//
// TestTheSessionTokenIsNeverLogged did not catch this, and could not: it
// captures cslog's writer, and none of this ever reached cslog.
//
// Silent unless the operator asked for debug. There is no middle setting that
// works — slow-query warnings carry the same interpolated SQL, so keeping them
// keeps the leak. At debug the operator has explicitly asked to see statements.
// gormConfig is what gorm.Open is given. It exists as a function so a test can
// hold the same value the connection does: with the logger set inline in the
// Open call, deleting that one field would put the leak back and every test
// here would still pass, because they would all still be exercising
// gormLogger() directly.
func gormConfig() *gorm.Config {
	return &gorm.Config{Logger: gormLogger()}
}

func gormLogger() gormlogger.Interface {
	level := gormlogger.Silent
	if cslog.GetLogger().GetLevel() >= logrus.DebugLevel {
		level = gormlogger.Info
	}
	return gormlogger.New(gormLogWriter{}, gormlogger.Config{
		SlowThreshold: 200 * time.Millisecond,
		LogLevel:      level,
		// A row that is not there is an answer, not a fault.
		IgnoreRecordNotFoundError: true,
		Colorful:                  false,
	})
}

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
		db, err = gorm.Open(postgres.Open(dsn), gormConfig())
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
