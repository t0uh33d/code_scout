package cslog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Options is how the process wants its logs written. Built from the
// configuration and handed to Configure once, at startup.
type Options struct {
	// Level is "debug", "info", "warn" or "error". Anything unrecognised falls
	// back to info rather than refusing to start: a typo in a log setting must
	// not be the reason a server will not boot.
	Level string

	// Format is "text", "json", or empty to decide by destination. Text is for
	// a person watching a console; JSON is for whatever greps or ships a file.
	Format string

	// File is where to write. Empty means stdout, which is what systemd and
	// Docker already capture and rotate, and is the right default for both.
	File string

	// Rotation, all ignored when File is empty.
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool
}

// Configure applies the settings to the process logger.
//
// Called once, immediately after the configuration is loaded, which is later
// than the first log line — the logger exists from package init so that a
// failure before this point still has somewhere to go.
//
// It returns an error only for things the operator can fix, such as a log
// directory it cannot create. Everything else degrades: an unknown level or
// format is reported and the default used.
func Configure(o Options) error {
	logger := oalog.logger

	level, err := logrus.ParseLevel(o.Level)
	if err != nil {
		level = logrus.InfoLevel
		logger.Warnf("Unknown log level %q, using info", o.Level)
	}
	logger.SetLevel(level)

	out := io.Writer(os.Stdout)
	toFile := o.File != ""
	if toFile {
		// Created up front so a bad path fails at startup with a clear message,
		// rather than silently swallowing every log line afterwards. Lumberjack
		// creates the file itself but not the directory.
		if err := os.MkdirAll(filepath.Dir(o.File), 0o755); err != nil {
			return fmt.Errorf("log directory %s: %w", filepath.Dir(o.File), err)
		}
		out = &lumberjack.Logger{
			Filename:   o.File,
			MaxSize:    o.MaxSizeMB,
			MaxBackups: o.MaxBackups,
			MaxAge:     o.MaxAgeDays,
			Compress:   o.Compress,
			LocalTime:  true,
		}
	}
	logger.SetOutput(out)

	logger.SetFormatter(formatterFor(o.Format, toFile))

	// Errors and fatals also go to stderr when the real output is a file.
	//
	// Without this, a process that dies on a fatal writes the reason into a file
	// and nothing else: `systemctl status` shows a dead unit and `journalctl`
	// shows nothing, which is exactly the shape of the outage this instance had
	// when a migration failed at boot.
	if toFile {
		logger.AddHook(&stderrHook{})
	}

	return nil
}

// formatterFor decides text or JSON.
//
// Empty means decide by where it is going: a console is being read by a person,
// a file is being read by a program. Naming a format overrides that, because
// somebody piping stdout into a collector wants JSON and somebody tailing a
// file wants to read it.
func formatterFor(format string, toFile bool) logrus.Formatter {
	switch format {
	case "json":
		return jsonFormatter()
	case "text":
		return textFormatter()
	case "":
		if toFile {
			return jsonFormatter()
		}
		return textFormatter()
	default:
		// Reported through the logger rather than returned, for the same reason
		// as the level: a typo here is not worth refusing to start over.
		oalog.logger.Warnf("Unknown log format %q, deciding by destination", format)
		return formatterFor("", toFile)
	}
}

func jsonFormatter() logrus.Formatter {
	return &logrus.JSONFormatter{
		TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime: "time",
			logrus.FieldKeyMsg:  "msg",
		},
	}
}

func textFormatter() logrus.Formatter {
	return &logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
		// Sorted so the same fields land in the same order on every line, which
		// is what makes a column of them scannable.
		DisableSorting: false,
	}
}

// stderrHook mirrors the levels somebody watching the service needs to see,
// using the logger's own formatter so the two copies read the same.
type stderrHook struct{}

func (h *stderrHook) Levels() []logrus.Level {
	return []logrus.Level{logrus.PanicLevel, logrus.FatalLevel, logrus.ErrorLevel}
}

func (h *stderrHook) Fire(entry *logrus.Entry) error {
	line, err := entry.Logger.Formatter.Format(entry)
	if err != nil {
		return err
	}
	_, err = os.Stderr.Write(line)
	return err
}
