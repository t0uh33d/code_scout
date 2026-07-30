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

	MySQLUser     string
	MySQLPassword string
	MySQLDatabase string
	MySQLHost     string
	MySQLPort     int

	// MySQLTLS maps to the driver's tls DSN parameter: "false" (default),
	// "true", "skip-verify", or "preferred". Managed databases such as RDS and
	// Cloud SQL often require this.
	MySQLTLS string

	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime int // minutes

	// PublicBaseURL is the URL users reach this instance on, used in the setup
	// snippet shown to developers. Defaults to http://host:port.
	PublicBaseURL string
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
		MySQLHost:       str(tree, "mysql_host", "CS_MYSQL_HOST", ""),
		MySQLPort:       num(tree, "mysql_port", "CS_MYSQL_PORT", 3306),
		MySQLUser:       str(tree, "mysql_user", "CS_MYSQL_USER", ""),
		MySQLPassword:   str(tree, "mysql_password", "CS_MYSQL_PASSWORD", ""),
		MySQLDatabase:   str(tree, "mysql_database", "CS_MYSQL_DATABASE", ""),
		MySQLTLS:        str(tree, "mysql_tls", "CS_MYSQL_TLS", "false"),
		MaxOpenConns:    num(tree, "max_open_conns", "CS_MAX_OPEN_CONNS", 25),
		MaxIdleConns:    num(tree, "max_idle_conns", "CS_MAX_IDLE_CONNS", 5),
		ConnMaxLifetime: num(tree, "conn_max_lifetime_minutes", "CS_CONN_MAX_LIFETIME_MINUTES", 30),
		PublicBaseURL:   str(tree, "public_base_url", "CS_PUBLIC_BASE_URL", ""),
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

	if Conf.PublicBaseURL == "" {
		Conf.PublicBaseURL = fmt.Sprintf("http://%s:%d", Conf.ServerHost, Conf.ServerPort)
	}
	Conf.PublicBaseURL = strings.TrimRight(Conf.PublicBaseURL, "/")

	return validate()
}

func validate() error {
	var missing []string
	if Conf.MySQLHost == "" {
		missing = append(missing, "mysql_host (CS_MYSQL_HOST)")
	}
	if Conf.MySQLUser == "" {
		missing = append(missing, "mysql_user (CS_MYSQL_USER)")
	}
	if Conf.MySQLDatabase == "" {
		missing = append(missing, "mysql_database (CS_MYSQL_DATABASE)")
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
	if c.MySQLPassword != "" {
		pw = "(set)"
	}
	return fmt.Sprintf("listen=%s:%d db=%s@%s:%d/%s tls=%s password=%s pool=%d/%d",
		c.ServerHost, c.ServerPort, c.MySQLUser, c.MySQLHost, c.MySQLPort,
		c.MySQLDatabase, c.MySQLTLS, pw, c.MaxIdleConns, c.MaxOpenConns)
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
