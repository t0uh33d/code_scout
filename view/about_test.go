package view

import (
	"strings"
	"testing"
	"time"

	"github.com/getcodescout/code_scout/app"
	"github.com/getcodescout/code_scout/internal/domain"
)

// The card has to tell four states apart, and the dangerous confusion is
// between "nothing newer exists" and "never managed to ask". Reporting the
// second as the first tells an operator they are current on no evidence.
func TestAboutCardDistinguishesEveryUpdateState(t *testing.T) {
	cases := []struct {
		name     string
		data     AboutData
		want     string
		notWant  []string
		hasNotes bool
	}{
		{
			name: "off",
			data: AboutData{Enabled: false},
			want: "Update checks are off",
			// Must not claim to know anything about releases while switched off.
			notWant: []string{"latest release", "is available"},
		},
		{
			name:    "on but never completed",
			data:    AboutData{Enabled: true},
			want:    "No check has completed yet",
			notWant: []string{"latest release", "is available"},
		},
		{
			name: "unreachable",
			data: AboutData{Enabled: true, Update: domain.VersionState{
				Err: "Could not reach GitHub: dial tcp: no route to host", CheckedAt: time.Now(),
			}},
			want:    "Could not reach GitHub",
			notWant: []string{"latest release", "is available"},
		},
		{
			// The state this instance is in until v1.0.0 is tagged. It must not
			// read as a fault, and it must not read as being up to date either.
			name: "no releases published yet",
			data: AboutData{Enabled: true, Update: domain.VersionState{
				Err: "No releases have been published yet.", CheckedAt: time.Now(),
			}},
			want:    "No releases have been published yet",
			notWant: []string{"Could not reach", "latest release", "is available"},
		},
		{
			name: "up to date",
			data: AboutData{Enabled: true, Update: domain.VersionState{
				Latest: app.Version, CheckedAt: time.Now(),
			}},
			want:    "This is the latest release",
			notWant: []string{"is available", "Could not reach"},
		},
		{
			name: "behind",
			data: AboutData{Enabled: true, Update: domain.VersionState{
				Latest: "9.9.9", Behind: true, CheckedAt: time.Now(),
				URL: "https://github.com/getcodescout/code_scout/releases/tag/v9.9.9",
			}},
			want:     "Version 9.9.9 is available",
			notWant:  []string{"latest release", "Could not reach"},
			hasNotes: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			html := render(t, AboutCard(c.data))

			if !strings.Contains(html, c.want) {
				t.Errorf("card does not say %q:\n%s", c.want, html)
			}
			for _, no := range c.notWant {
				if strings.Contains(html, no) {
					t.Errorf("card also says %q, which contradicts %q", no, c.want)
				}
			}
			if got := strings.Contains(html, "Release notes"); got != c.hasNotes {
				t.Errorf("Release notes link = %v, want %v", got, c.hasNotes)
			}
		})
	}
}

// The card is swapped by outerHTML, so the id it targets has to be on the
// element the response replaces. Lose it and the first swap works and the
// second silently does nothing, because there is no longer anything to hit.
func TestAboutCardCarriesItsSwapTarget(t *testing.T) {
	html := render(t, AboutCard(AboutData{Enabled: true}))

	if !strings.Contains(html, `id="about-card"`) {
		t.Errorf("the card has no id, so hx-target=\"#about-card\" hits nothing:\n%s", html)
	}
	for _, want := range []string{
		`hx-target="#about-card"`,
		`hx-swap="outerHTML"`,
		// A checkbox has no submit event; without this it posts on click,
		// which fires for label clicks that changed nothing.
		`hx-trigger="change"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %s:\n%s", want, html)
		}
	}
}

// Check now is meaningless while the setting is off, and an enabled button that
// silently does nothing is worse than a disabled one.
func TestCheckNowIsDisabledWhenTheSettingIsOff(t *testing.T) {
	// The bare attribute, not the substring: the button's class list carries
	// disabled:opacity-40, so "does the HTML contain 'disabled'" is true in
	// both states and proves nothing.
	const attr = `hx-disabled-elt="this" disabled `

	off := render(t, AboutCard(AboutData{Enabled: false}))
	if !strings.Contains(off, attr) {
		t.Errorf("Check now is live while update checks are off:\n%s", off)
	}

	on := render(t, AboutCard(AboutData{Enabled: true}))
	if strings.Contains(on, attr) {
		t.Errorf("Check now is disabled while the setting is on:\n%s", on)
	}
}

// The version is the thing to quote in a bug report, and the commit is what
// distinguishes two builds of the same version.
func TestAboutCardShowsBuildIdentity(t *testing.T) {
	html := render(t, AboutCard(AboutData{Enabled: true}))
	for _, want := range []string{app.Version, app.Commit, app.BuildTime} {
		if !strings.Contains(html, want) {
			t.Errorf("About card is missing %q:\n%s", want, html)
		}
	}
}
