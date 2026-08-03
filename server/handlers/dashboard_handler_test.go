package handlers

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/getcodescout/code_scout/internal/domain"
)

// TestParseMembersOnlyAcceptsOfferedAccounts is the whole point of recomputing
// the candidate list on submit: the ids arrive from a form, so an id the wizard
// never offered must be dropped rather than trusted.
func TestParseMembersOnlyAcceptsOfferedAccounts(t *testing.T) {
	offered := uuid.New()
	notOffered := uuid.New()
	candidates := []domain.User{{ID: offered}}

	form := url.Values{}
	form.Add("member", offered.String())
	form.Add("member", notOffered.String())
	form.Add("member", "not-a-uuid")
	form.Set("level_"+offered.String(), "maintainer")
	form.Set("level_"+notOffered.String(), "maintainer")

	r := httptest.NewRequest("POST", "/dashboard/projects", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatalf("parse form: %v", err)
	}

	got := parseMembers(r, candidates)
	if len(got) != 1 {
		t.Fatalf("want 1 assignment, got %d: %+v", len(got), got)
	}
	if got[0].UserID != offered {
		t.Fatalf("kept the wrong account: %s", got[0].UserID)
	}
	if got[0].Level != domain.LevelMaintainer {
		t.Fatalf("want maintainer, got %q", got[0].Level)
	}
}

// An unticked box sends no "member" value at all, so a level left on maintainer
// for someone who was never selected must not add them.
func TestParseMembersIgnoresUntickedAccounts(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	candidates := []domain.User{{ID: a}, {ID: b}}

	form := url.Values{}
	form.Add("member", a.String())
	form.Set("level_"+a.String(), "viewer")
	form.Set("level_"+b.String(), "maintainer")

	r := httptest.NewRequest("POST", "/dashboard/projects", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatalf("parse form: %v", err)
	}

	got := parseMembers(r, candidates)
	if len(got) != 1 || got[0].UserID != a || got[0].Level != domain.LevelViewer {
		t.Fatalf("want only %s as viewer, got %+v", a, got)
	}
}

// A level that is missing or garbage falls back to viewer, never to the more
// powerful maintainer.
func TestParseMembersFallsBackToViewer(t *testing.T) {
	id := uuid.New()
	candidates := []domain.User{{ID: id}}

	form := url.Values{}
	form.Add("member", id.String())
	form.Set("level_"+id.String(), "owner")

	r := httptest.NewRequest("POST", "/dashboard/projects", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatalf("parse form: %v", err)
	}

	got := parseMembers(r, candidates)
	if len(got) != 1 || got[0].Level != domain.LevelViewer {
		t.Fatalf("want viewer fallback, got %+v", got)
	}
}
