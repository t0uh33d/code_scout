package cslog

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

// capture points the process logger at a buffer and puts it back afterwards.
// The logger is package state, so a test that forgets this leaks into the next.
func capture(t *testing.T) *bytes.Buffer {
	t.Helper()
	logger := GetLogger()
	oldOut, oldLevel, oldFmt := logger.Out, logger.Level, logger.Formatter
	oldHooks := logger.Hooks
	t.Cleanup(func() {
		logger.SetOutput(oldOut)
		logger.SetLevel(oldLevel)
		logger.SetFormatter(oldFmt)
		logger.ReplaceHooks(oldHooks)
	})

	buf := &bytes.Buffer{}
	logger.SetOutput(buf)
	logger.ReplaceHooks(logrus.LevelHooks{})
	return buf
}

func TestLevelDecidesWhatIsWritten(t *testing.T) {
	for _, c := range []struct {
		level     string
		wantDebug bool
		wantInfo  bool
	}{
		{"debug", true, true},
		{"info", false, true},
		{"warn", false, false},
		// A typo must not stop the server, and must not silently become debug
		// either — that is how a token ends up in a log. It falls back to info.
		{"not-a-level", false, true},
	} {
		t.Run(c.level, func(t *testing.T) {
			buf := capture(t)
			if err := Configure(Options{Level: c.level}); err != nil {
				t.Fatalf("configure: %v", err)
			}
			// Configure sets its own output; point it back at the buffer.
			GetLogger().SetOutput(buf)

			GetLogger().Debug("DB: GetProjectByID")
			GetLogger().Info("Request")

			if got := strings.Contains(buf.String(), "GetProjectByID"); got != c.wantDebug {
				t.Errorf("debug line written = %v, want %v", got, c.wantDebug)
			}
			if got := strings.Contains(buf.String(), "Request"); got != c.wantInfo {
				t.Errorf("info line written = %v, want %v", got, c.wantInfo)
			}
		})
	}
}

// duration_ms has to survive as a number. A string passes any "contains 14"
// check and then cannot be sorted, filtered or averaged by anything reading the
// log, which is the entire reason it stopped being time.Duration's String().
func TestJSONFormatKeepsNumbersAsNumbers(t *testing.T) {
	buf := capture(t)
	GetLogger().SetFormatter(jsonFormatter())
	GetLogger().SetLevel(logrus.InfoLevel)

	GetLogger().WithFields(logrus.Fields{
		"request_id":  "req-8f2a",
		"status":      200,
		"duration_ms": 14.207,
	}).Info("Request")

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("the json formatter did not produce json: %v\n%s", err, buf.String())
	}

	if _, ok := line["duration_ms"].(float64); !ok {
		t.Errorf("duration_ms is %T, want a number: %v", line["duration_ms"], line["duration_ms"])
	}
	if _, ok := line["status"].(float64); !ok {
		t.Errorf("status is %T, want a number", line["status"])
	}
	for _, key := range []string{"time", "level", "msg", "request_id"} {
		if _, ok := line[key]; !ok {
			t.Errorf("json line has no %q: %s", key, buf.String())
		}
	}
}

// Empty means decide by destination: a console is read by a person, a file by a
// program. Naming one overrides that.
func TestFormatIsChosenByDestination(t *testing.T) {
	cases := []struct {
		format   string
		toFile   bool
		wantJSON bool
	}{
		{"", false, false},
		{"", true, true},
		{"json", false, true},
		{"text", true, false},
		{"nonsense", true, true},
	}
	for _, c := range cases {
		_, isJSON := formatterFor(c.format, c.toFile).(*logrus.JSONFormatter)
		if isJSON != c.wantJSON {
			t.Errorf("formatterFor(%q, toFile=%v) json = %v, want %v",
				c.format, c.toFile, isJSON, c.wantJSON)
		}
	}
}

func TestConfigureWritesToTheFileAndRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "code_scout.log")

	capture(t)
	if err := Configure(Options{
		Level: "info", File: path,
		MaxSizeMB: 1, MaxBackups: 3, MaxAgeDays: 1,
	}); err != nil {
		t.Fatalf("configure: %v", err)
	}

	// Comfortably past 1 MB so rotation has to happen at least once.
	big := strings.Repeat("x", 4096)
	for i := 0; i < 400; i++ {
		GetLogger().WithField("filler", big).Info("Request")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) < 2 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected the log to rotate, found only %v", names)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the live log file is missing: %v", err)
	}
	if len(body) == 0 {
		t.Error("the live log file is empty")
	}
}

// A log directory that cannot be created is worth refusing to start over: the
// alternative is a server that runs and silently writes its logs nowhere.
func TestConfigureFailsOnAnUnusableLogPath(t *testing.T) {
	capture(t)

	// A file where a directory needs to be.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := Configure(Options{Level: "info", File: filepath.Join(blocker, "code_scout.log")})
	if err == nil {
		t.Error("an unusable log path was accepted, so the server would start and log nowhere")
	}
}

// With a file configured, errors still have to reach stderr.
//
// Without this, a process that dies on a fatal writes the reason into a file
// and nothing else: `systemctl status` shows a dead unit and `journalctl -u`
// shows no reason. That is the exact shape of the outage this instance had when
// a migration failed at boot.
func TestErrorsAlsoReachStderrWhenWritingToAFile(t *testing.T) {
	capture(t)

	realStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = realStderr })

	path := filepath.Join(t.TempDir(), "code_scout.log")
	if err := Configure(Options{Level: "info", File: path, MaxSizeMB: 1}); err != nil {
		t.Fatalf("configure: %v", err)
	}

	GetLogger().Info("this one stays in the file")
	GetLogger().Error("ERROR: the database went away")

	w.Close()
	var mirrored bytes.Buffer
	if _, err := mirrored.ReadFrom(r); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(mirrored.String(), "the database went away") {
		t.Errorf("the error never reached stderr, so journalctl would show nothing:\n%s", mirrored.String())
	}
	if strings.Contains(mirrored.String(), "stays in the file") {
		t.Error("info lines are being duplicated to stderr, which doubles the volume")
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the log file: %v", err)
	}
	if !strings.Contains(string(body), "the database went away") {
		t.Error("the error is on stderr but missing from the file it was configured to go to")
	}
}
