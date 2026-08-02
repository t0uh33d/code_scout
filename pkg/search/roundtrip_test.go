package search

import (
	"testing"

	"github.com/t0uh33d/code_scout/internal/domain"
)

// Every toolbar control computes the next query from the current filter, so a
// filter that cannot survive being written out and read back would lose a term
// on the second click. These are the queries the toolbar actually produces.
func TestQueryRoundTrips(t *testing.T) {
	queries := []string{
		"",
		"level:error",
		"level:error level:fatal",
		"tag:auth",
		"tag:auth tag:checkout",
		"-tag:heartbeat",
		"tag:checkout -tag:heartbeat",
		"is:network",
		"last:24h",
		"level:error tag:auth -tag:noise is:network last:7d",
		"timeout",
		`"gateway timed out"`,
		`level:error "gateway timed out"`,
	}

	for _, q := range queries {
		t.Run(q, func(t *testing.T) {
			first, err := Parse(q)
			if err != nil {
				t.Fatalf("parse %q: %v", q, err)
			}
			written := first.Query()

			second, err := Parse(written)
			if err != nil {
				t.Fatalf("re-parse %q: %v", written, err)
			}
			if again := second.Query(); again != written {
				t.Errorf("not stable: %q -> %q -> %q", q, written, again)
			}
		})
	}
}

// The same set of clicks in a different order must produce the same URL, or
// history fills with entries that mean the same thing.
func TestQueryIsOrderIndependent(t *testing.T) {
	a, _ := Parse("level:fatal level:error tag:z tag:a")
	b, _ := Parse("level:error level:fatal tag:a tag:z")
	if a.Query() != b.Query() {
		t.Errorf("order changed the query: %q vs %q", a.Query(), b.Query())
	}
}

// Clicking one level when nothing is filtered should narrow to that level,
// which is what a filter control is for.
func TestLevelToggleNarrowsFirst(t *testing.T) {
	empty := domain.SearchFilter{}
	got := empty.WithLevelToggled("error")
	if q := got.Query(); q != "level:error" {
		t.Errorf("first click should narrow to that level, got %q", q)
	}

	// Clicking it again removes the only level, which means unfiltered rather
	// than a viewer that can only ever be empty.
	back := got.WithLevelToggled("error")
	if q := back.Query(); q != "" {
		t.Errorf("clicking the only active level off should clear the filter, got %q", q)
	}
	for _, level := range domain.LogLevels {
		if !back.HasLevel(level) {
			t.Errorf("%s should show again once nothing is filtered", level)
		}
	}
}

func TestLevelToggleAddsAndRemoves(t *testing.T) {
	f, _ := Parse("level:error")
	withFatal := f.WithLevelToggled("fatal")
	if q := withFatal.Query(); q != "level:error level:fatal" {
		t.Errorf("want both levels, got %q", q)
	}
	if q := withFatal.WithLevelToggled("error").Query(); q != "level:fatal" {
		t.Errorf("want fatal alone, got %q", q)
	}
}

// Neutral, included, excluded, neutral. Four clicks must return to where it
// started or the chip becomes a one-way door.
func TestTagCyclesThroughThreeStates(t *testing.T) {
	f := domain.SearchFilter{}

	f = f.WithTagCycled("noise")
	if f.StateForTag("noise") != domain.TagIncluded || f.Query() != "tag:noise" {
		t.Fatalf("first click should include, got %q", f.Query())
	}

	f = f.WithTagCycled("noise")
	if f.StateForTag("noise") != domain.TagExcluded || f.Query() != "-tag:noise" {
		t.Fatalf("second click should exclude, got %q", f.Query())
	}

	f = f.WithTagCycled("noise")
	if f.StateForTag("noise") != domain.TagNeutral || f.Query() != "" {
		t.Fatalf("third click should clear, got %q", f.Query())
	}
}

// A tag must never be in both lists, which the cycle guarantees by removing
// from one as it adds to the other.
func TestTagIsNeverBothIncludedAndExcluded(t *testing.T) {
	f := domain.SearchFilter{}
	for i := 0; i < 10; i++ {
		f = f.WithTagCycled("noise")
		for _, in := range f.Tags {
			for _, out := range f.ExcludeTags {
				if in == out {
					t.Fatalf("after %d clicks %q is both included and excluded", i+1, in)
				}
			}
		}
	}
}

func TestWindowTogglesOff(t *testing.T) {
	f, _ := Parse("last:24h")
	if q := f.WithWindow("7d").Query(); q != "last:7d" {
		t.Errorf("a different window should replace, got %q", q)
	}
	if q := f.WithWindow("24h").Query(); q != "" {
		t.Errorf("the same window should clear, got %q", q)
	}
}

func TestNetworkOnlyToggles(t *testing.T) {
	f := domain.SearchFilter{}
	on := f.WithNetworkOnly()
	if on.Query() != "is:network" {
		t.Errorf("want is:network, got %q", on.Query())
	}
	if off := on.WithNetworkOnly().Query(); off != "" {
		t.Errorf("want it cleared, got %q", off)
	}
}

// The toggles return new values. If they shared a backing array, toggling one
// filter would change another that was derived from it.
func TestTogglesDoNotMutateTheOriginal(t *testing.T) {
	f, _ := Parse("level:error tag:auth")
	before := f.Query()

	_ = f.WithLevelToggled("fatal")
	_ = f.WithTagCycled("checkout")
	_ = f.WithTagCycled("auth")

	if after := f.Query(); after != before {
		t.Errorf("the original filter changed: %q -> %q", before, after)
	}
}
