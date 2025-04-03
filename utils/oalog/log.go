package oalog

import (
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

type Logger struct {
	logger *logrus.Logger
}

func getLine() string {
	_, fileName, fileLine, ok := runtime.Caller(2)
	names := strings.Split(fileName, "codescout_api")
	name := names[0]
	if len(names) > 1 {
		name = names[len(names)-1]
	}
	var s string
	if ok {
		s = fmt.Sprintf(time.Now().Format("02/01/06 15:04:05")+" %s:%d-> ", name, fileLine)
	} else {
		s = ""
	}
	return s
}

var oalog = EnableLogFiles()

func Info(args ...interface{}) {
	args = append([]interface{}{getLine()}, args...)
	oalog.logger.Info(args...)
}

func Error(args ...interface{}) {
	args = append([]interface{}{getLine()}, args...)
	oalog.logger.Error(args...)
}

func Warn(args ...interface{}) {
	args = append([]interface{}{getLine()}, args...)
	oalog.logger.Warn(args...)
}

func Debug(args ...interface{}) {
	args = append([]interface{}{getLine()}, args...)
	oalog.logger.Debug(args...)
}

func Debugf(str string, args ...interface{}) {
	oalog.logger.Debugf(fmt.Sprint(getLine(), str), args...)
}

func Infof(str string, args ...interface{}) {
	oalog.logger.Infof(fmt.Sprint(getLine(), str), args...)
}

func Errorf(str string, args ...interface{}) {
	oalog.logger.Errorf(fmt.Sprint(getLine(), str), args...)
}

func Warnf(str string, args ...interface{}) {
	oalog.logger.Warnf(fmt.Sprint(getLine(), str), args...)
}

func Fatal(args ...interface{}) {
	args = append([]interface{}{getLine()}, args...)
	oalog.logger.Fatal(args...)
}

func Out() io.Writer {
	return oalog.logger.Out
}

func EnableLogFiles() *Logger {
	logger := logrus.New()
	logger.Level = logrus.DebugLevel
	lg := new(Logger)

	lg.logger = logger
	return lg
}
