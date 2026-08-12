package mcptools

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/google/uuid"
)

func TestCursorSurvivesARoundTrip(t *testing.T) {
	in := &domain.LogCursor{Time: time.Now().UTC(), ID: uuid.New()}
	out, err := decodeCursor(encodeCursor(in))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.Time.Equal(in.Time) || out.ID != in.ID {
		t.Errorf("round trip changed the cursor: %+v -> %+v", in, out)
	}
}

func TestAMangledCursorIsARefusal(t *testing.T) {
	for _, raw := range []string{"garbage", "2026-01-01|not-a-uuid", "not-a-time|" + uuid.NewString(), "|"} {
		if _, err := decodeCursor(raw); err == nil {
			t.Errorf("cursor %q was accepted", raw)
		}
	}
	// Absent is not mangled: the first page has no cursor.
	if c, err := decodeCursor(""); err != nil || c != nil {
		t.Errorf("an empty cursor should mean the first page, got %v %v", c, err)
	}
}

func TestClampLimitAppliesDefaultAndCeiling(t *testing.T) {
	if got := clampLimit(0, 50, 200); got != 50 {
		t.Errorf("zero should mean the default, got %d", got)
	}
	if got := clampLimit(-3, 50, 200); got != 50 {
		t.Errorf("negative should mean the default, got %d", got)
	}
	if got := clampLimit(9999, 50, 200); got != 200 {
		t.Errorf("over the ceiling should mean the ceiling, got %d", got)
	}
	if got := clampLimit(7, 50, 200); got != 7 {
		t.Errorf("an in-range ask should stand, got %d", got)
	}
}

func TestALongMessageIsTruncatedAndFlagged(t *testing.T) {
	// Multi-byte runes on purpose: a byte-index cut would split one.
	long := strings.Repeat("héllo ", 1000)
	l := domain.Log{ID: uuid.New(), SessionID: uuid.New(), Message: long}

	listed := listLog(l)
	if !listed.MessageTruncated {
		t.Fatal("a long message was not flagged")
	}
	if got := len([]rune(listed.Message)); got != messageBudget {
		t.Errorf("truncated to %d runes, want %d", got, messageBudget)
	}
	if !strings.HasPrefix(long, listed.Message) {
		t.Error("the truncated message is not a prefix of the original")
	}

	whole := wholeLog(l)
	if whole.MessageTruncated || whole.Message != long {
		t.Error("the whole fetch truncated the message")
	}
}

func TestOversizeMetadataIsOmittedWholeAndFlagged(t *testing.T) {
	big := json.RawMessage(`{"body":"` + strings.Repeat("x", metadataBudget) + `"}`)
	l := domain.Log{ID: uuid.New(), SessionID: uuid.New(), Metadata: &big}

	listed := listLog(l)
	if listed.Metadata != nil {
		t.Error("oversize metadata was included in a list")
	}
	if !listed.MetadataOmitted || listed.MetadataBytes != len(big) {
		t.Errorf("omission not flagged honestly: omitted=%v bytes=%d", listed.MetadataOmitted, listed.MetadataBytes)
	}

	// The decoded value must carry the whole body, not a marker.
	whole := wholeLog(l)
	if whole.MetadataOmitted {
		t.Error("the whole fetch omitted the metadata")
	}
	if m, ok := whole.Metadata.(map[string]any); !ok || m["body"] != strings.Repeat("x", metadataBudget) {
		t.Error("the whole fetch did not carry the full metadata")
	}

	// Small metadata rides along, decoded.
	small := json.RawMessage(`{"k":"v"}`)
	l.Metadata = &small
	got := listLog(l)
	if got.MetadataOmitted {
		t.Error("small metadata should not be omitted")
	}
	if m, ok := got.Metadata.(map[string]any); !ok || m["k"] != "v" {
		t.Errorf("small metadata should be included, got %#v", got.Metadata)
	}
}
