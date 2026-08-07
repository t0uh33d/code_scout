package confs

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml"
)

// ConfigFilePath is where the optional TOML file is read from. Environment
// variables take precedence over anything in it.
const ConfigFilePath = "/etc/code-scout.conf"

type Configuration struct {
	ServerHost string
	ServerPort int

	DBUser     string
	DBPassword string
	DBName     string
	DBHost     string
	DBPort     int

	// DBSSLMode maps to libpq sslmode: "disable" (default), "require",
	// "verify-ca" or "verify-full". Managed databases such as RDS and
	// Cloud SQL usually need at least "require".
	DBSSLMode string

	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime int // minutes

	// PublicBaseURL is the URL a developer's app should send logs to, shown in
	// the setup snippet. Leave it empty and the address the operator reached the
	// dashboard on is used instead. Set it explicitly when the dashboard and the
	// SDK reach this instance by different names, for example behind a reverse
	// proxy or when the dashboard is on an internal hostname.
	PublicBaseURL string

	// Logging. Empty LogFile means stdout, which systemd and Docker both
	// already capture and rotate, and is the right default for both. Set it and
	// the server owns the file and rotates it itself; do not also point
	// logrotate at it, or two rotators fight over the same file.
	LogLevel      string
	LogFormat     string
	LogFile       string
	LogMaxSizeMB  int
	LogMaxBackups int
	LogMaxAgeDays int
	LogCompress   bool
}

var Conf Configuration

// Load reads configuration from the TOML file if present, then applies any
// environment overrides. It returns an error rather than falling back to a
// default database, because silently starting against the wrong database is
// worse than refusing to start.
func Load() error {
	tree, err := toml.LoadFile(ConfigFilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("config: could not read %s: %v", ConfigFilePath, err)
		}
		tree = &toml.Tree{}
	}

	Conf = Configuration{
		ServerHost:      str(tree, "host", "CS_HOST", "0.0.0.0"),
		ServerPort:      num(tree, "port", "CS_PORT", 24275),
		DBHost:          str(tree, "db_host", "CS_DB_HOST", ""),
		DBPort:          num(tree, "db_port", "CS_DB_PORT", 5432),
		DBUser:          str(tree, "db_user", "CS_DB_USER", ""),
		DBPassword:      str(tree, "db_password", "CS_DB_PASSWORD", ""),
		DBName:          str(tree, "db_name", "CS_DB_NAME", ""),
		DBSSLMode:       str(tree, "db_sslmode", "CS_DB_SSLMODE", "disable"),
		MaxOpenConns:    num(tree, "max_open_conns", "CS_MAX_OPEN_CONNS", 25),
		MaxIdleConns:    num(tree, "max_idle_conns", "CS_MAX_IDLE_CONNS", 5),
		ConnMaxLifetime: num(tree, "conn_max_lifetime_minutes", "CS_CONN_MAX_LIFETIME_MINUTES", 30),
		PublicBaseURL:   str(tree, "public_base_url", "CS_PUBLIC_BASE_URL", ""),

		// info, not debug. It was hardcoded to debug, which put every database
		// call in the journal and buried the lines worth reading.
		LogLevel:  str(tree, "log_level", "CS_LOG_LEVEL", "info"),
		LogFormat: str(tree, "log_format", "CS_LOG_FORMAT", ""),
		LogFile:   str(tree, "log_file", "CS_LOG_FILE", ""),

		LogMaxSizeMB:  num(tree, "log_max_size_mb", "CS_LOG_MAX_SIZE_MB", 100),
		LogMaxBackups: num(tree, "log_max_backups", "CS_LOG_MAX_BACKUPS", 7),
		LogMaxAgeDays: num(tree, "log_max_age_days", "CS_LOG_MAX_AGE_DAYS", 30),
		LogCompress:   boolean(tree, "log_compress", "CS_LOG_COMPRESS", true),
	}

	// PORT and HOST without the prefix are what most platforms inject.
	if v := os.Getenv("PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			Conf.ServerPort = n
		}
	}
	if v := os.Getenv("HOST"); v != "" {
		Conf.ServerHost = v
	}

	// Deliberately not defaulted to http://host:port. ServerHost is a bind
	// address, so that produces http://0.0.0.0:24275, which no phone can reach.
	// An empty value means "ask the request", which is at least an address that
	// demonstrably routes here.
	Conf.PublicBaseURL = strings.TrimRight(Conf.PublicBaseURL, "/")

	return validate()
}

func validate() error {
	var missing []string
	if Conf.DBHost == "" {
		missing = append(missing, "db_host (CS_DB_HOST)")
	}
	if Conf.DBUser == "" {
		missing = append(missing, "db_user (CS_DB_USER)")
	}
	if Conf.DBName == "" {
		missing = append(missing, "db_name (CS_DB_NAME)")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required database configuration: %s\n"+
			"set these as environment variables or in %s", strings.Join(missing, ", "), ConfigFilePath)
	}
	return nil
}

// Redacted renders the effective configuration for logging, without the password.
func (c Configuration) Redacted() string {
	pw := "(empty)"
	if c.DBPassword != "" {
		pw = "(set)"
	}
	return fmt.Sprintf("listen=%s:%d db=%s@%s:%d/%s sslmode=%s password=%s pool=%d/%d",
		c.ServerHost, c.ServerPort, c.DBUser, c.DBHost, c.DBPort,
		c.DBName, c.DBSSLMode, pw, c.MaxIdleConns, c.MaxOpenConns)
}

func str(tree *toml.Tree, key, env, fallback string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	if v, ok := tree.Get(key).(string); ok && v != "" {
		return v
	}
	return fallback
}

// boolean accepts what strconv.ParseBool does: 1, t, T, TRUE, true, True and
// their false counterparts. Anything else is reported and the default kept,
// because a mistyped log setting is not worth refusing to start over.
func boolean(tree *toml.Tree, key, env string, fallback bool) bool {
	if v := os.Getenv(env); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
		log.Printf("config: %s=%q is not a true/false value, ignoring", env, v)
	}
	if v, ok := tree.Get(key).(bool); ok {
		return v
	}
	return fallback
}

func num(tree *toml.Tree, key, env string, fallback int) int {
	if v := os.Getenv(env); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		log.Printf("config: %s=%q is not a number, ignoring", env, v)
	}
	if v, ok := tree.Get(key).(int64); ok {
		return int(v)
	}
	return fallback
}
