package view

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
)

// A session filter has no toggle of its own — a project has thousands of users
// and no full set to render controls from — so the chip is the only way back
// out of one. A filter with no exit is a filter people reload the page to
// escape.
func TestSessionChipsRenderWithAWayOut(t *testing.T) {
	projectID := uuid.New()
	f := domain.SearchFilter{
		Levels: []string{"error"},
		Session: domain.SessionScope{
			User:   "u_8812",
			Device: "Pixel 7",
		},
	}

	out := render(t, LogFilters(projectID, f, nil))

	if !strings.Contains(out, `data-session-filter="user"`) {
		t.Error("the user filter should render a chip")
	}
	if !strings.Contains(out, `data-session-filter="device"`) {
		t.Error("the device filter should render a chip")
	}
	if !strings.Contains(out, "u_8812") || !strings.Contains(out, "Pixel 7") {
		t.Error("a chip should show the value it is filtering on")
	}

	// The user chip's link keeps the device filter and the level filter, and
	// drops only the user. Checking the markup rather than the filter object,
	// because the href is what a click actually follows.
	if !strings.Contains(out, "device%3A") {
		t.Error("clearing one session filter should keep the others in the link")
	}
	if !strings.Contains(out, "level%3Aerror") {
		t.Error("clearing a session filter should keep the level filter")
	}
}

func TestNoSessionChipsWhenNoneApplied(t *testing.T) {
	out := render(t, LogFilters(uuid.New(), domain.SearchFilter{Levels: []string{"error"}}, nil))

	if strings.Contains(out, "data-session-filter") {
		t.Error("a filter with no session scope should render no session chips")
	}
	if strings.Contains(out, ">Who<") {
		t.Error("the Who row should not appear when there is nothing in it")
	}
}

// Telling someone to send logs from their app when they have thousands, and a
// filter is what emptied the screen, sends them to the wrong place entirely.
func TestEmptyStateDistinguishesNoLogsFromNoMatches(t *testing.T) {
	projectID := uuid.New()

	fresh := render(t, LogViewerEmpty(projectID, domain.SearchFilter{}))
	if !strings.Contains(fresh, "No logs yet") {
		t.Error("an unfiltered empty project should say it has no logs yet")
	}
	if strings.Contains(fresh, "Clear filters") {
		t.Error("there is nothing to clear when nothing is filtered")
	}

	filtered := render(t, LogViewerEmpty(projectID, domain.SearchFilter{
		Session: domain.SessionScope{User: "u_nobody"},
	}))
	if strings.Contains(filtered, "No logs yet") {
		t.Error("a filtered empty view must not claim the project has no logs")
	}
	if !strings.Contains(filtered, "Nothing matches this filter") {
		t.Error("a filtered empty view should say the filter matched nothing")
	}
	if !strings.Contains(filtered, "Clear filters") {
		t.Error("a filtered empty view should offer a way back")
	}
}
