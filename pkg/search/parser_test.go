package search

import (
	"testing"
	"time"

	"github.com/getcodescout/code_scout/internal/domain"
)

func TestParseEmpty(t *testing.T) {
	f, err := Parse("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Levels) > 0 || f.TextQuery != "" || len(f.Tags) > 0 {
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
	if len(f.Levels) != 1 || f.Levels[0] != "error" {
		t.Fatalf("expected levels=[error], got: %v", f.Levels)
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
	if len(f.Levels) != 1 || f.Levels[0] != "error" {
		t.Fatalf("expected levels=[error], got %v", f.Levels)
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

// Levels repeat and OR together, because the toolbar's toggles build the query
// by appending one term per level that is switched on.
func TestParseRepeatedLevels(t *testing.T) {
	f, err := Parse("level:error level:fatal")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Levels) != 2 || f.Levels[0] != "error" || f.Levels[1] != "fatal" {
		t.Fatalf("expected [error fatal], got %v", f.Levels)
	}
}

// Clicking a toggle twice must not add the term twice, or the query string
// grows without bound while meaning the same thing.
func TestParseDeduplicates(t *testing.T) {
	f, err := Parse("level:error level:error tag:auth tag:auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Levels) != 1 {
		t.Errorf("levels should de-duplicate, got %v", f.Levels)
	}
	if len(f.Tags) != 1 {
		t.Errorf("tags should de-duplicate, got %v", f.Tags)
	}
}

func TestParseExcludedTags(t *testing.T) {
	f, err := Parse("tag:checkout -tag:heartbeat -tag:noise")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Tags) != 1 || f.Tags[0] != "checkout" {
		t.Fatalf("expected included [checkout], got %v", f.Tags)
	}
	if len(f.ExcludeTags) != 2 || f.ExcludeTags[0] != "heartbeat" || f.ExcludeTags[1] != "noise" {
		t.Fatalf("expected excluded [heartbeat noise], got %v", f.ExcludeTags)
	}
}

// The same tag cannot be both, and the chip cycles through one state at a time,
// so whichever term appears is the one that applies.
func TestParseTagStates(t *testing.T) {
	f, _ := Parse("tag:auth -tag:noise")
	if got := f.StateForTag("auth"); got != domain.TagIncluded {
		t.Errorf("auth should be included, got %v", got)
	}
	if got := f.StateForTag("noise"); got != domain.TagExcluded {
		t.Errorf("noise should be excluded, got %v", got)
	}
	if got := f.StateForTag("other"); got != domain.TagNeutral {
		t.Errorf("an unmentioned tag should be neutral, got %v", got)
	}
}

// No level terms means every level shows, which is what an unfiltered view is.
func TestHasLevelDefaultsToEverything(t *testing.T) {
	f, _ := Parse("")
	for _, level := range domain.LogLevels {
		if !f.HasLevel(level) {
			t.Errorf("%s should show when nothing is filtered", level)
		}
	}

	f, _ = Parse("level:error")
	if !f.HasLevel("error") {
		t.Error("error should show")
	}
	if f.HasLevel("info") {
		t.Error("info should be hidden when only error is selected")
	}
}

func TestParseWindows(t *testing.T) {
	for _, label := range WindowLabels {
		f, err := Parse("last:" + label)
		if err != nil {
			t.Fatalf("last:%s should parse: %v", label, err)
		}
		if f.Since == nil {
			t.Fatalf("last:%s should set a lower bound", label)
		}
		if f.SinceLabel != label {
			t.Errorf("last:%s should remember its label, got %q", label, f.SinceLabel)
		}
		if !f.Since.Before(time.Now()) {
			t.Errorf("last:%s should look backwards", label)
		}
	}

	if _, err := Parse("last:banana"); err == nil {
		t.Error("an unknown window should be refused rather than silently ignored")
	}
}

// Excluding a level is just not including it, so the negation is refused with a
// message rather than parsed into something that quietly does nothing.
func TestParseRefusesNegationOnNonTags(t *testing.T) {
	for _, q := range []string{"-level:error", "-is:network", "-last:24h", "-bareword"} {
		if _, err := Parse(q); err == nil {
			t.Errorf("%q should be refused", q)
		}
	}
}

// A lone minus is a search for "-", not a negation of nothing.
func TestParseLoneMinusIsText(t *testing.T) {
	f, err := Parse("-")
	if err != nil {
		t.Fatalf("a lone minus should be text, got error: %v", err)
	}
	if f.TextQuery != "-" {
		t.Errorf("expected the minus as text, got %q", f.TextQuery)
	}
}
