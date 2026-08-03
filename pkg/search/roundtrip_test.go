package search

import (
	"testing"

	"github.com/getcodescout/code_scout/internal/domain"
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
		// Session-scoped. Device models and OS strings contain spaces, so these
		// exercise the quoting on ordinary values rather than only on
		// fingerprints.
		"user:u_8812",
		`device:"Pixel 7"`,
		`os:"iOS 17.4"`,
		"app_version:3.11.2",
		"installation:4f2a81b0-1c2d-4e88-9f60-a3f2c7d14e88",
		`level:error user:u_8812 device:"Pixel 7" app_version:3.11.2`,
	}

	// Fingerprints are not typed, they are linked to from the Errors screen, and
	// they contain everything the tokeniser would otherwise choke on. If one of
	// these fails to round trip, clicking any filter on top of it silently drops
	// the group and shows the whole project's logs instead.
	for _, fp := range fingerprintSamples {
		f := domain.SearchFilter{Fingerprint: &fp}
		queries = append(queries, f.Query())
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

// Real fingerprints, as domain.Fingerprint produces them. Every one of these
// carries at least one character the tokeniser treats as structure.
var fingerprintSamples = []string{
	"User {n} not found",                 // spaces
	"SocketException: Failed host lookup", // a colon, which splits field from value
	"POST /v{n}/checkout/{id}",            // slashes and braces
	`Missing key "{str}" in config`,        // quotes, from a message that quoted something
	`He said "hello`,                      // an unbalanced quote the normaliser leaves alone
	`path C:\Users\{id}\tmp`,              // backslashes, the escape character itself
	"Timeout",                             // nothing special at all
}

// The Query() stability check above proves the string is stable. This proves it
// still means the same thing: a fingerprint that came back subtly different
// would match nothing, and the log viewer would show an empty list for a group
// the Errors screen says has 47 occurrences.
func TestFingerprintSurvivesTheRoundTrip(t *testing.T) {
	for _, fp := range fingerprintSamples {
		t.Run(fp, func(t *testing.T) {
			written := domain.SearchFilter{Fingerprint: &fp}.Query()
			back, err := Parse(written)
			if err != nil {
				t.Fatalf("parse %q: %v", written, err)
			}
			if back.Fingerprint == nil {
				t.Fatalf("fingerprint was lost by %q", written)
			}
			if *back.Fingerprint != fp {
				t.Errorf("fingerprint changed: %q -> %q -> %q", fp, written, *back.Fingerprint)
			}
		})
	}
}

// A quoted token stays text even when it looks like a field, so searching for
// the literal text "level:error" is still possible.
func TestQuotedTokenIsAlwaysText(t *testing.T) {
	f, err := Parse(`"level:error"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.Levels) != 0 {
		t.Errorf("a quoted token should not set a field, got levels %v", f.Levels)
	}
	if f.TextQuery != "level:error" {
		t.Errorf("want text 'level:error', got %q", f.TextQuery)
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

// A device model with a space in it must survive being written into a URL and
// read back. Without quoting, `device:Pixel 7` parses as device "Pixel" plus a
// bare word "7", and the filter silently means something else.
func TestSessionValuesWithSpacesSurvive(t *testing.T) {
	cases := []struct {
		name  string
		build domain.SearchFilter
		read  func(*domain.SearchFilter) string
	}{
		{"device", domain.SearchFilter{Session: domain.SessionScope{Device: "Pixel 7 Pro"}},
			func(f *domain.SearchFilter) string { return f.Session.Device }},
		{"os", domain.SearchFilter{Session: domain.SessionScope{OS: "iOS 17.4"}},
			func(f *domain.SearchFilter) string { return f.Session.OS }},
		{"user with a colon", domain.SearchFilter{Session: domain.SessionScope{User: "tenant:8812"}},
			func(f *domain.SearchFilter) string { return f.Session.User }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			written := tc.build.Query()
			back, err := Parse(written)
			if err != nil {
				t.Fatalf("parse %q: %v", written, err)
			}
			want := tc.read(&tc.build)
			if got := tc.read(back); got != want {
				t.Errorf("value changed: %q -> %q -> %q", want, written, got)
			}
			if back.TextQuery != "" {
				t.Errorf("part of the value leaked into free text: %q", back.TextQuery)
			}
		})
	}
}

// Repeating a field replaces rather than accumulates. Two devices at once would
// match nothing, so a repeat is someone correcting themselves.
func TestRepeatedSessionFieldTakesTheLast(t *testing.T) {
	f, err := Parse("device:Pixel device:iPhone")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if f.Session.Device != "iPhone" {
		t.Errorf("device = %q, want the last one typed", f.Session.Device)
	}
}

// Each chip has to clear its own field and leave the rest of the filter alone,
// or removing one filter would quietly drop another.
func TestSessionChipClearsOnlyItself(t *testing.T) {
	f, err := Parse(`level:error user:u_8812 device:"Pixel 7" app_version:3.11.2`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	chips := f.SessionChips()
	if len(chips) != 3 {
		t.Fatalf("want a chip per applied session filter, got %d", len(chips))
	}

	for _, chip := range chips {
		back, err := Parse(chip.Without)
		if err != nil {
			t.Fatalf("parse %q: %v", chip.Without, err)
		}
		// The one it names is gone.
		if got := valueFor(back.Session, chip.Field); got != "" {
			t.Errorf("%s chip left its own value behind: %q", chip.Field, got)
		}
		// Everything else survives.
		if len(back.Levels) != 1 || back.Levels[0] != "error" {
			t.Errorf("%s chip dropped the level filter: %v", chip.Field, back.Levels)
		}
		for _, other := range chips {
			if other.Field == chip.Field {
				continue
			}
			if valueFor(back.Session, other.Field) != other.Value {
				t.Errorf("clearing %s also cleared %s", chip.Field, other.Field)
			}
		}
	}
}

func valueFor(s domain.SessionScope, field string) string {
	switch field {
	case "user":
		return s.User
	case "device":
		return s.Device
	case "os":
		return s.OS
	case "app_version":
		return s.AppVersion
	case "installation":
		return s.Installation
	}
	return ""
}

// Nothing session-scoped means the log query never has to reach into sessions
// at all, which is what keeps the common case a single-table read.
func TestScopeIsEmptyWhenNothingIsSet(t *testing.T) {
	f, _ := Parse("level:error tag:auth is:network last:24h")
	if f.Session.Any() {
		t.Error("an ordinary filter should not be session-scoped")
	}

	scoped, _ := Parse("user:u_8812")
	if !scoped.Session.Any() {
		t.Error("user: should be session-scoped")
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
