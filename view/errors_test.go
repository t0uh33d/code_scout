package view

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
)

func sampleGroup() domain.ErrorGroup {
	trace := json.RawMessage(`[{"index":0,"method":"CartBloc._resolveDiscount","path":"package:ledger/cart/bloc.dart","line":212,"column":31}]`)
	return domain.ErrorGroup{
		Fingerprint:     "User {n} not found",
		SampleMessage:   "User 4821 not found",
		Level:           "error",
		Count:           18,
		Sessions:        6,
		FirstSeen:       time.Now().Add(-72 * time.Hour),
		LastSeen:        time.Now().Add(-2 * time.Minute),
		LatestLogID:     uuid.New(),
		LatestSessionID: uuid.New(),
		StackTrace:      &trace,
	}
}

// The row must show a real message, not the normalised key. A row reading
// "User {n} not found" is the internals leaking onto the screen.
func TestErrorRowShowsTheSampleMessage(t *testing.T) {
	g := sampleGroup()
	out := render(t, errorGroupRow(uuid.New(), g))

	if !contains(out, "User 4821 not found") {
		t.Errorf("want the sample message on the row, got %s", out)
	}
	if contains(out, ">User {n} not found<") {
		t.Errorf("the fingerprint should not be the visible title, got %s", out)
	}
}

// The whole point of the screen: how often, and across how many sessions.
func TestErrorRowCountsOccurrencesAndSessions(t *testing.T) {
	out := render(t, errorGroupRow(uuid.New(), sampleGroup()))

	if !contains(out, "×18") {
		t.Errorf("want the occurrence count, got %s", out)
	}
	if !contains(out, "6 sessions") {
		t.Errorf("want the session count, got %s", out)
	}
}

// "View in logs" has to carry the exact group. If the fingerprint is dropped or
// mangled by the URL, the link lands on the project's whole log stream and
// quietly looks like it worked.
func TestViewInLogsLinksToTheGroup(t *testing.T) {
	projectID := uuid.New()
	g := sampleGroup()
	out := render(t, errorGroupRow(projectID, g))

	want := filterHref(projectID, domain.SearchFilter{Fingerprint: &g.Fingerprint})
	if !contains(out, want) {
		t.Errorf("want a link to %s, got %s", want, out)
	}
	if !contains(out, "/project/"+projectID.String()+"/session/"+g.LatestSessionID.String()) {
		t.Errorf("want a link to the latest session, got %s", out)
	}
}

// A group with one occurrence should not read "1 sessions".
func TestSessionsLabelIsSingular(t *testing.T) {
	if got := sessionsLabel(1); got != "1 session" {
		t.Errorf("want '1 session', got %q", got)
	}
	if got := sessionsLabel(2); got != "2 sessions" {
		t.Errorf("want '2 sessions', got %q", got)
	}
}

func TestFormatStackTrace(t *testing.T) {
	frames := json.RawMessage(`[
		{"index":0,"method":"CartBloc._resolveDiscount","path":"package:ledger/cart/bloc.dart","line":212,"column":31},
		{"index":1,"method":"CartBloc._onApplyCode","path":"package:ledger/cart/bloc.dart","line":181,"column":18}
	]`)
	want := "#0  CartBloc._resolveDiscount (package:ledger/cart/bloc.dart:212:31)\n" +
		"#1  CartBloc._onApplyCode (package:ledger/cart/bloc.dart:181:18)"
	if got := formatStackTrace(&frames); got != want {
		t.Errorf("want:\n%s\ngot:\n%s", want, got)
	}

	// A trace in some other shape is still worth reading, so it is shown as
	// stored rather than swallowed.
	odd := json.RawMessage(`{"note":"not an array"}`)
	if got := formatStackTrace(&odd); got != string(odd) {
		t.Errorf("an unexpected shape should be shown raw, got %q", got)
	}

	empty := json.RawMessage(`[]`)
	if got := formatStackTrace(&empty); got != "" {
		t.Errorf("an empty trace should render nothing, got %q", got)
	}
	if got := formatStackTrace(nil); got != "" {
		t.Errorf("a missing trace should render nothing, got %q", got)
	}
}

func TestRelTime(t *testing.T) {
	cases := map[time.Duration]string{
		10 * time.Second: "just now",
		14 * time.Minute: "14m ago",
		3 * time.Hour:    "3h ago",
		72 * time.Hour:   "3d ago",
		-1 * time.Minute: "just now", // a device clock running fast
	}
	for d, want := range cases {
		if got := relTime(time.Now().Add(-d)); got != want {
			t.Errorf("%s ago: want %q, got %q", d, want, got)
		}
	}
}
