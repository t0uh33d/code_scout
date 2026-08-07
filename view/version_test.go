package view

import (
	"strings"
	"testing"
	"time"

	"github.com/getcodescout/code_scout/app"
	"github.com/getcodescout/code_scout/internal/domain"
)

// The update state is package-level, so a test that sets it has to put it back
// or it leaks into whichever test runs next.
func withUpdateState(t *testing.T, s domain.VersionState) {
	t.Helper()
	before := UpdateState()
	SetUpdateState(s)
	t.Cleanup(func() { SetUpdateState(before) })
}

func behind(latest string) domain.VersionState {
	return domain.VersionState{
		Latest:    latest,
		URL:       "https://github.com/getcodescout/code_scout/releases/tag/v" + latest,
		CheckedAt: time.Now(),
		Behind:    true,
	}
}

func TestVersionTagAlwaysShowsTheVersion(t *testing.T) {
	withUpdateState(t, domain.VersionState{})

	html := render(t, versionTag(&domain.User{Role: domain.RoleMember}))
	if !strings.Contains(html, "v"+app.Version) {
		t.Errorf("version tag does not show %s:\n%s", app.Version, html)
	}
}

// Everything below is one question: who gets nudged. The badge is an
// instruction to go and upgrade a server, so it goes to the people who can.
func TestUpdateBadgeAudience(t *testing.T) {
	cases := []struct {
		name  string
		user  *domain.User
		state domain.VersionState
		want  bool
	}{
		{"super admin, behind", &domain.User{Role: domain.RoleSuperAdmin}, behind("1.2.0"), true},
		{"admin, behind", &domain.User{Role: domain.RoleAdmin}, behind("1.2.0"), false},
		{"member, behind", &domain.User{Role: domain.RoleMember}, behind("1.2.0"), false},
		{"signed out, behind", nil, behind("1.2.0"), false},
		{"super admin, up to date", &domain.User{Role: domain.RoleSuperAdmin}, domain.VersionState{Latest: app.Version}, false},
		// Never checked is not the same claim as up to date, but it draws the
		// same: nothing. An instance with no route out must not accuse itself.
		{"super admin, never checked", &domain.User{Role: domain.RoleSuperAdmin}, domain.VersionState{}, false},
		// A failed check must not leak its error into the chrome. The settings
		// card is where an operator goes to find out why.
		{"super admin, check failed", &domain.User{Role: domain.RoleSuperAdmin}, domain.VersionState{Err: "dial tcp: no route to host"}, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withUpdateState(t, c.state)

			html := render(t, versionTag(c.user))
			got := strings.Contains(html, "releases/tag/")

			if got != c.want {
				t.Errorf("badge shown = %v, want %v\n%s", got, c.want, html)
			}
			if strings.Contains(html, "no route to host") {
				t.Errorf("the check's error leaked into the page:\n%s", html)
			}
		})
	}
}

// The commit and build time are the whole point of the hover: "which build" is
// a different question from "which version", and only the commit answers it.
func TestVersionTagCarriesBuildDetail(t *testing.T) {
	withUpdateState(t, domain.VersionState{})

	html := render(t, versionTag(&domain.User{Role: domain.RoleSuperAdmin}))
	for _, want := range []string{app.Version, app.Commit, app.BuildTime} {
		if !strings.Contains(html, want) {
			t.Errorf("build detail is missing %q:\n%s", want, html)
		}
	}
}
