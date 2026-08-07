package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/getcodescout/code_scout/app"
	"github.com/getcodescout/code_scout/internal/domain"
)

// githubStub answers like the releases endpoint and counts what it was asked.
// The count is the point: several of these tests are about a request that must
// not happen, and asserting on the resulting state would pass even if it did.
type githubStub struct {
	hits   atomic.Int32
	tag    string
	status int
	body   string
}

func (g *githubStub) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.hits.Add(1)
		if g.status != 0 && g.status != http.StatusOK {
			w.WriteHeader(g.status)
			return
		}
		if g.body != "" {
			w.Write([]byte(g.body))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name": g.tag,
			"html_url": "https://github.com/getcodescout/code_scout/releases/tag/" + g.tag,
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newTestVersion wires the service to a stub instead of GitHub.
func newTestVersion(t *testing.T, settings *InstanceSettingsService, g *githubStub) *VersionService {
	t.Helper()
	svc := NewVersionService(settings)
	svc.url = g.server(t).URL
	return svc
}

func TestCheckFindsANewerRelease(t *testing.T) {
	settings, _ := newTestSettings(t)
	g := &githubStub{tag: "v99.0.0"}
	svc := newTestVersion(t, settings, g)

	state := svc.Check(context.Background())

	if !state.Behind {
		t.Errorf("running %s against 99.0.0, Behind = false", app.Version)
	}
	if state.Latest != "99.0.0" {
		t.Errorf("Latest = %q, want 99.0.0 (the v must be stripped)", state.Latest)
	}
	if state.Err != "" {
		t.Errorf("unexpected error: %s", state.Err)
	}
	if state.CheckedAt.IsZero() {
		t.Error("CheckedAt was not stamped, so the card cannot say when it last asked")
	}
}

func TestCheckOnTheCurrentVersionIsNotBehind(t *testing.T) {
	settings, _ := newTestSettings(t)
	svc := newTestVersion(t, settings, &githubStub{tag: "v" + app.Version})

	if svc.Check(context.Background()).Behind {
		t.Error("an instance on the latest release reported itself behind")
	}
}

// The whole point of the setting. A request that goes out anyway makes the
// switch a lie, and the state alone cannot tell you: a disabled check that
// still fetched would look identical to one that skipped.
func TestCheckMakesNoRequestWhenDisabled(t *testing.T) {
	settings, _ := newTestSettings(t)
	if _, err := settings.SetUpdateCheckEnabled(context.Background(), false); err != nil {
		t.Fatalf("disable: %v", err)
	}

	g := &githubStub{tag: "v99.0.0"}
	svc := newTestVersion(t, settings, g)
	svc.Check(context.Background())

	if n := g.hits.Load(); n != 0 {
		t.Errorf("the check is off and it still called GitHub %d times", n)
	}
}

// Load fails open, so an unreadable settings row leaves the cache on defaults,
// and the default is "check". Not knowing whether the operator turned it off is
// not permission to leave it on.
func TestCheckMakesNoRequestWhenSettingsNeverLoaded(t *testing.T) {
	// Deliberately not Load()ed, which is what a database that was down at boot
	// leaves behind.
	settings := NewInstanceSettingsService(&fakeSettingsRepo{stored: domain.DefaultInstanceSettings()})
	if settings.Loaded() {
		t.Fatal("premise is wrong: the service reports loaded before Load ran")
	}
	if !settings.Current().UpdateCheckEnabled {
		t.Fatal("premise is wrong: the default is off, so this test proves nothing")
	}

	g := &githubStub{tag: "v99.0.0"}
	svc := newTestVersion(t, settings, g)
	svc.Check(context.Background())

	if n := g.hits.Load(); n != 0 {
		t.Errorf("settings were never read and it called GitHub %d times", n)
	}
}

// A failed check must leave the instance quiet, not accusing itself of being
// out of date on no evidence.
func TestCheckFailureIsRecordedAndNotBehind(t *testing.T) {
	settings, _ := newTestSettings(t)

	for _, c := range []struct {
		name string
		stub *githubStub
		// want is a phrase the message must contain. The card renders Err
		// verbatim, so these strings are what an operator actually reads.
		want string
	}{
		{"rate limited", &githubStub{status: http.StatusForbidden}, "rate limiting"},
		{"server error", &githubStub{status: http.StatusInternalServerError}, "Could not reach GitHub"},
		{"body is not json", &githubStub{body: "<html>gateway timeout</html>"}, "Could not reach GitHub"},
		{"release has no tag", &githubStub{body: `{"html_url":"https://example.com"}`}, "Could not reach GitHub"},
		// The state this repository is actually in until v1.0.0 is tagged.
		// releases/latest answers 404 when nothing has been published, and
		// reporting that as a failure would have every fresh instance showing
		// an alarming message about a situation that is completely normal.
		{"no releases published", &githubStub{status: http.StatusNotFound}, "No releases have been published yet"},
	} {
		t.Run(c.name, func(t *testing.T) {
			state := newTestVersion(t, settings, c.stub).Check(context.Background())

			if state.Err == "" {
				t.Fatal("the failure was not recorded, so the card cannot explain itself")
			}
			if !strings.Contains(state.Err, c.want) {
				t.Errorf("Err = %q, want it to contain %q", state.Err, c.want)
			}
			if state.Behind {
				t.Error("a failed check reported the instance out of date")
			}
			if state.Latest != "" {
				t.Errorf("Latest = %q after a failure, want empty", state.Latest)
			}
		})
	}
}

// Two conditions that both fail, and must not read the same. "Nothing has been
// released" is not a problem with this instance; "the network is down" is.
func TestNoReleasesDoesNotReadAsAFailure(t *testing.T) {
	settings, _ := newTestSettings(t)

	none := newTestVersion(t, settings, &githubStub{status: http.StatusNotFound}).Check(context.Background())
	if strings.Contains(none.Err, "Could not reach") {
		t.Errorf("a repository with no releases reports a connection problem: %q", none.Err)
	}

	broken := newTestVersion(t, settings, &githubStub{status: http.StatusBadGateway}).Check(context.Background())
	if none.Err == broken.Err {
		t.Errorf("no-releases and unreachable produce the same message: %q", none.Err)
	}
}

// String comparison is the specific bug this guards. "1.9.0" > "1.10.0" as
// text, so a naive check tells an instance on 1.10.0 to downgrade.
func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"1.10.0", "1.9.0", true},
		{"1.9.0", "1.10.0", false},
		{"2.0.0", "1.99.99", true},
		{"1.0.1", "1.0.0", true},
		{"1.0.0", "1.0.0", false},
		{"v1.2.0", "1.1.0", true},
		{"1.2.0", "v1.1.0", true},
		// A prerelease is not an upgrade over the release it precedes.
		{"1.1.0-rc1", "1.1.0", false},
		{"1.1.0", "1.1.0-rc1", true},
		// Nonsense is never an upgrade. A mistyped tag should go unnoticed
		// rather than nag every operator to install it.
		{"", "1.0.0", false},
		{"banana", "1.0.0", false},
		{"1.0.0", "banana", false},
	}

	for _, c := range cases {
		if got := IsNewer(c.latest, c.current); got != c.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}
