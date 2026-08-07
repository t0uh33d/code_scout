package cslog

import (
	"io"

	"github.com/sirupsen/logrus"
)

type Logger struct {
	logger *logrus.Logger
}

// newLogger builds the process logger.
//
// It runs from a package var, before any configuration has been read, so it
// starts permissive and on stderr: anything that goes wrong between here and
// Configure is a startup failure, and stderr is where an operator looks for
// those. Configure then replaces the level, format and destination.
//
// It was called EnableLogFiles, which it has never done.
func newLogger() *Logger {
	logger := logrus.New()
	logger.Level = logrus.InfoLevel
	// The same formatter Configure would pick for a console, so the handful of
	// lines written before the configuration is read do not arrive stamped in a
	// different format from everything after them.
	logger.SetFormatter(textFormatter())
	return &Logger{logger: logger}
}

// GetLogger returns the underlying logrus.Logger instance.
func GetLogger() *logrus.Logger {
	return oalog.logger
}

// The package-level helpers, for the handful of places that log outside a
// request: startup, shutdown, the database connection, panic recovery.
//
// They used to prefix every message with a timestamp and a source location
// built by hand. The location was produced by splitting the file path on
// "codescout_api", a module name this project has not used in years, so the
// split never matched and the whole absolute path from the build machine was
// printed into production logs. The timestamp was a second one, in a different
// format, on a line logrus had already stamped.
func Info(args ...interface{})  { oalog.logger.Info(args...) }
func Error(args ...interface{}) { oalog.logger.Error(args...) }
func Warn(args ...interface{})  { oalog.logger.Warn(args...) }
func Debug(args ...interface{}) { oalog.logger.Debug(args...) }
func Fatal(args ...interface{}) { oalog.logger.Fatal(args...) }

func Debugf(format string, args ...interface{}) { oalog.logger.Debugf(format, args...) }
func Infof(format string, args ...interface{})  { oalog.logger.Infof(format, args...) }
func Errorf(format string, args ...interface{}) { oalog.logger.Errorf(format, args...) }
func Warnf(format string, args ...interface{})  { oalog.logger.Warnf(format, args...) }

func Out() io.Writer {
	return oalog.logger.Out
}
