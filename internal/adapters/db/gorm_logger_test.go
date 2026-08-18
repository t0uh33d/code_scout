package db

import (
	"bytes"
	"context"
	"io"
	stdlog "log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/getcodescout/code_scout/pkg/cslog"
)

// The statement a session lookup actually produces, with a live credential in
// it. GORM interpolates parameters before logging, so this is the shape that
// reaches the writer, not the ? placeholders the code passes.
const sessionLookupSQL = `SELECT * FROM "user_sessions" WHERE token = 'cs-live-9f2a41b8-secret' AND "deleted_at" IS NULL`

const theToken = "cs-live-9f2a41b8-secret"

// captureStdout runs fn with os.Stdout redirected and returns what was written.
//
// This is the stream that matters and the one the older
// TestTheSessionTokenIsNeverLogged cannot see: it swaps cslog's writer, while
// GORM's default logger holds its own log.Logger pointed straight at
// os.Stdout. The leak lived in the gap between those two facts.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()

	os.Stdout = old
	w.Close()
	out := <-done
	r.Close()
	return out
}

// Everything else here exercises gormLogger() directly, which would keep
// passing if the Logger field were dropped from the config gorm.Open is handed.
// This is the one that holds the wiring: it traces a missed session lookup
// through whatever logger the connection would actually get.
func TestTheConnectionsOwnLoggerDoesNotLeak(t *testing.T) {
	logger := cslog.GetLogger()
	oldOut, oldLevel := logger.Out, logger.Level
	t.Cleanup(func() { logger.SetOutput(oldOut); logger.SetLevel(oldLevel) })

	var viaCslog bytes.Buffer
	logger.SetOutput(&viaCslog)
	logger.SetLevel(logrus.InfoLevel)

	cfg := gormConfig()
	if cfg.Logger == nil {
		t.Fatal("gorm.Open would be handed a config with no logger, so GORM " +
			"falls back to its default, which prints failed statements to stdout")
	}

	cfg.Logger.Trace(
		context.Background(),
		time.Now().Add(-5*time.Millisecond),
		func() (string, int64) { return sessionLookupSQL, 0 },
		gorm.ErrRecordNotFound,
	)

	if strings.Contains(viaCslog.String(), theToken) {
		t.Errorf("the connection's logger leaked the session token:\n%s", viaCslog.String())
	}
}

func TestARecordNotFoundNeverPrintsTheStatement(t *testing.T) {
	logger := cslog.GetLogger()
	oldOut, oldLevel := logger.Out, logger.Level
	t.Cleanup(func() { logger.SetOutput(oldOut); logger.SetLevel(oldLevel) })

	var viaCslog bytes.Buffer
	logger.SetOutput(&viaCslog)
	// info, the production default. Debug is the operator explicitly asking to
	// see statements, and is covered separately below.
	logger.SetLevel(logrus.InfoLevel)

	stdout := captureStdout(t, func() {
		gormLogger().Trace(
			context.Background(),
			time.Now().Add(-5*time.Millisecond),
			func() (string, int64) { return sessionLookupSQL, 0 },
			gorm.ErrRecordNotFound,
		)
	})

	if strings.Contains(stdout, theToken) {
		t.Errorf("the session token reached stdout:\n%s", stdout)
	}
	if strings.Contains(viaCslog.String(), theToken) {
		t.Errorf("the session token reached the log:\n%s", viaCslog.String())
	}
}

// A slow query carries the same interpolated SQL as a failed one, so it is the
// same leak by another route. Every request with a valid cookie runs this
// lookup, so a database under load is all it takes.
func TestASlowQueryNeverPrintsTheStatement(t *testing.T) {
	logger := cslog.GetLogger()
	oldOut, oldLevel := logger.Out, logger.Level
	t.Cleanup(func() { logger.SetOutput(oldOut); logger.SetLevel(oldLevel) })

	var viaCslog bytes.Buffer
	logger.SetOutput(&viaCslog)
	logger.SetLevel(logrus.InfoLevel)

	stdout := captureStdout(t, func() {
		gormLogger().Trace(
			context.Background(),
			// Well past GORM's 200ms slow threshold.
			time.Now().Add(-2*time.Second),
			func() (string, int64) { return sessionLookupSQL, 1 },
			nil,
		)
	})

	if strings.Contains(stdout, theToken) {
		t.Errorf("a slow query put the session token on stdout:\n%s", stdout)
	}
	if strings.Contains(viaCslog.String(), theToken) {
		t.Errorf("a slow query put the session token in the log:\n%s", viaCslog.String())
	}
}

// The counterfactual, so the tests above cannot quietly start passing for the
// wrong reason.
//
// It rebuilds GORM's default *configuration* against a buffer rather than
// using gormlogger.Default itself. Default is constructed at package init with
// log.New(os.Stdout, ...), which captures the file descriptor at that moment,
// so reassigning os.Stdout in a test cannot redirect it — an earlier version of
// this test swapped os.Stdout, saw nothing, and concluded there was no leak.
// The config is what the claim is about anyway: Warn plus
// IgnoreRecordNotFoundError:false is what turns a missed session lookup into a
// printed statement.
//
// If this stops leaking, GORM changed its defaults, and the reasoning in
// connection.go wants re-reading rather than the fix dropping.
func TestGormsOwnDefaultConfigWouldHaveLeakedIt(t *testing.T) {
	var buf bytes.Buffer
	asDefault := gormlogger.New(
		stdlog.New(&buf, "\r\n", stdlog.LstdFlags),
		gormlogger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  gormlogger.Warn,
			IgnoreRecordNotFoundError: false,
			Colorful:                  false,
		},
	)

	asDefault.Trace(
		context.Background(),
		time.Now().Add(-5*time.Millisecond),
		func() (string, int64) { return sessionLookupSQL, 0 },
		gorm.ErrRecordNotFound,
	)

	if !strings.Contains(buf.String(), theToken) {
		t.Fatalf("GORM's default configuration no longer prints a failed "+
			"statement, so the reasoning in connection.go needs revisiting. "+
			"It wrote:\n%s", buf.String())
	}
}

// At debug the operator has asked to see statements, and gets them through
// cslog rather than on stdout, so log_file and log_level still hold.
func TestAtDebugStatementsGoThroughCslogNotStdout(t *testing.T) {
	logger := cslog.GetLogger()
	oldOut, oldLevel := logger.Out, logger.Level
	t.Cleanup(func() { logger.SetOutput(oldOut); logger.SetLevel(oldLevel) })

	var viaCslog bytes.Buffer
	logger.SetOutput(&viaCslog)
	logger.SetLevel(logrus.DebugLevel)

	stdout := captureStdout(t, func() {
		gormLogger().Trace(
			context.Background(),
			time.Now().Add(-5*time.Millisecond),
			func() (string, int64) { return "SELECT 1", 1 },
			nil,
		)
	})

	if strings.Contains(stdout, "SELECT 1") {
		t.Errorf("GORM wrote to stdout instead of through cslog:\n%s", stdout)
	}
	if !strings.Contains(viaCslog.String(), "SELECT 1") {
		t.Errorf("debug asked for statements and none arrived via cslog:\n%s", viaCslog.String())
	}
}
