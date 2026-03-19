package search

import (
	"testing"
)

func TestParseEmpty(t *testing.T) {
	f, err := Parse("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Level != nil || f.TextQuery != "" || len(f.Tags) > 0 {
		t.Fatalf("expected empty filter, got: %+v", f)
	}
}

func TestParseWhitespace(t *testing.T) {
	f, err := Parse("   ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.TextQuery != "" {
		t.Fatalf("expected empty text query, got: %q", f.TextQuery)
	}
}

func TestParseLevelFilter(t *testing.T) {
	f, err := Parse("level:error")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Level == nil || *f.Level != "error" {
		t.Fatalf("expected level=error, got: %v", f.Level)
	}
}

func TestParseTagFilter(t *testing.T) {
	f, err := Parse("tag:auth tag:payment")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Tags) != 2 || f.Tags[0] != "auth" || f.Tags[1] != "payment" {
		t.Fatalf("expected tags [auth, payment], got: %v", f.Tags)
	}
}

func TestParseCombined(t *testing.T) {
	f, err := Parse(`level:error tag:auth "timeout"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Level == nil || *f.Level != "error" {
		t.Fatalf("expected level=error")
	}
	if len(f.Tags) != 1 || f.Tags[0] != "auth" {
		t.Fatalf("expected tags [auth], got: %v", f.Tags)
	}
	if f.TextQuery != "timeout" {
		t.Fatalf("expected text query 'timeout', got: %q", f.TextQuery)
	}
}

func TestParseIsNetwork(t *testing.T) {
	f, err := Parse("is:network")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.IsNetwork == nil || !*f.IsNetwork {
		t.Fatalf("expected IsNetwork=true")
	}
}

func TestParseBareText(t *testing.T) {
	f, err := Parse("something happened here")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.TextQuery != "something happened here" {
		t.Fatalf("expected 'something happened here', got: %q", f.TextQuery)
	}
}

func TestParseSessionUUID(t *testing.T) {
	f, err := Parse("session:a1b2c3d4-e5f6-7890-abcd-1234567890ab")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.SessionID == nil || f.SessionID.String() != "a1b2c3d4-e5f6-7890-abcd-1234567890ab" {
		t.Fatalf("expected session UUID, got: %v", f.SessionID)
	}
}

func TestParseErrorMissingValue(t *testing.T) {
	_, err := Parse("level:")
	if err == nil {
		t.Fatal("expected error for missing value")
	}
	pe, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("expected ParseError, got: %T", err)
	}
	if pe.Position != 6 {
		t.Fatalf("expected position 6, got: %d", pe.Position)
	}
}

func TestParseErrorUnclosedQuote(t *testing.T) {
	_, err := Parse(`"unclosed quote`)
	if err == nil {
		t.Fatal("expected error for unclosed quote")
	}
	pe, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("expected ParseError, got: %T", err)
	}
	if pe.Position != 0 {
		t.Fatalf("expected position 0, got: %d", pe.Position)
	}
}

func TestParseErrorUnknownField(t *testing.T) {
	_, err := Parse("foo:bar")
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
	pe, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("expected ParseError, got: %T", err)
	}
	if pe.Position != 0 {
		t.Fatalf("expected position 0, got: %d", pe.Position)
	}
}

func TestParseErrorInvalidLevel(t *testing.T) {
	_, err := Parse("level:banana")
	if err == nil {
		t.Fatal("expected error for invalid level")
	}
}
