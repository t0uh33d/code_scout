package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/semver"

	"github.com/getcodescout/code_scout/app"
	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/pkg/cslog"
)

// VersionService asks GitHub whether a newer release exists.
//
// It holds the answer in memory and nowhere else. This is a cache, not a
// setting: it is derived entirely from a public fact, it goes stale, and a
// restart asking again costs one request. Giving it a column would create a
// second thing that can disagree with GitHub.
type VersionService struct {
	settings *InstanceSettingsService
	client   *http.Client
	url      string

	mu    sync.RWMutex
	state domain.VersionState
}

// errNoReleases is the pre-first-release state: the repository exists and has
// published nothing. Its own value because the About card says something
// different and calmer about it than about a failure.
var errNoReleases = errors.New("no releases published yet")

// checkTimeout bounds the whole request. Short on purpose: this runs on a
// schedule and on a button, and nothing waits on the result. An instance behind
// a firewall that blackholes outbound traffic hangs until this fires, and doing
// that for a minute on every boot would be its own bug report.
const checkTimeout = 5 * time.Second

func NewVersionService(settings *InstanceSettingsService) *VersionService {
	return &VersionService{
		settings: settings,
		client:   &http.Client{Timeout: checkTimeout},
		url:      domain.UpdateCheckURL,
	}
}

// Current is what the instance last learned. The zero value means it has not
// managed to ask, which the UI must not render as "up to date".
func (s *VersionService) Current() domain.VersionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// Check asks GitHub for the latest release and records the answer.
//
// It returns the new state so the caller can hand it to the view. Errors are
// recorded on the state rather than returned: nothing upstream can act on a
// failed update check, and a caller that had to handle an error would be
// tempted to log it every ten minutes forever.
func (s *VersionService) Check(ctx context.Context) domain.VersionState {
	log := cslog.L(ctx)

	// Two reasons to make no request, and the second is the interesting one.
	//
	// Load fails open, so an unreadable settings row leaves the cache on
	// defaults — and the default here is "yes, check". For a display timezone
	// that is the right way to fail. For an operator's stated preference about
	// outbound traffic it is not: not knowing whether they turned it off is not
	// permission to leave it on. So this skips rather than guesses, the same
	// shape as retention refusing to delete against settings it never read.
	if !s.settings.Loaded() {
		log.Debug("Update check skipped: instance settings were never loaded")
		return s.Current()
	}
	if !s.settings.Current().UpdateCheckEnabled {
		return s.Current()
	}

	latest, url, err := s.fetch(ctx)

	state := domain.VersionState{CheckedAt: time.Now()}
	if err != nil {
		// Err is a finished sentence, not a fragment for the card to decorate.
		// The card cannot tell "no releases exist yet" from "the network is
		// down", and a caller that prefixed everything with "Could not reach
		// GitHub" would turn the first into a contradiction.
		//
		// Kept, not logged at error: an instance with no route out would
		// otherwise write an error line every day forever about a condition
		// nobody considers broken.
		if errors.Is(err, errNoReleases) {
			state.Err = "No releases have been published yet."
		} else {
			state.Err = "Could not reach GitHub: " + err.Error()
		}
		log.WithError(err).Debug("Update check failed")
	} else {
		state.Latest = latest
		state.URL = url
		state.Behind = IsNewer(latest, app.Version)
	}

	s.mu.Lock()
	s.state = state
	s.mu.Unlock()
	return state
}

func (s *VersionService) fetch(ctx context.Context) (version, url string, err error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return "", "", err
	}
	// The documented way to pin GitHub's API version. Without it the response
	// shape is whatever their default is on the day.
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "code_scout/"+app.Version)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		// releases/latest answers 404 when a repository has published no
		// releases at all, which is a different thing from a broken request and
		// is exactly what this repository returns today. Saying "GitHub
		// answered 404" would send an operator looking for a typo in a URL that
		// is fine.
		return "", "", errNoReleases
	case http.StatusForbidden, http.StatusTooManyRequests:
		// Unauthenticated GitHub allows 60 requests an hour per IP. One
		// instance cannot exhaust that; an office behind one NAT address can.
		// "GitHub said 403" sends people hunting for a broken token that does
		// not exist.
		return "", "", fmt.Errorf("GitHub is rate limiting this address, try later")
	default:
		return "", "", fmt.Errorf("GitHub answered %s", resp.Status)
	}

	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Draft   bool   `json:"draft"`
		Pre     bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", err
	}
	if release.TagName == "" {
		return "", "", fmt.Errorf("the latest release has no tag")
	}

	return strings.TrimPrefix(release.TagName, "v"), release.HTMLURL, nil
}

// IsNewer reports whether latest is a later release than current.
//
// Both arrive without the "v" that x/mod/semver requires, so it is put back
// before comparing. String comparison is the trap this exists to avoid: "1.9.0"
// sorts after "1.10.0", so a plain != or > would tell an instance on 1.10.0
// that 1.9.0 is an upgrade.
//
// Anything unparseable is not an upgrade. A malformed tag on the release should
// go unnoticed rather than nag every operator to install it.
func IsNewer(latest, current string) bool {
	l, c := "v"+strings.TrimPrefix(latest, "v"), "v"+strings.TrimPrefix(current, "v")
	if !semver.IsValid(l) || !semver.IsValid(c) {
		return false
	}
	return semver.Compare(l, c) > 0
}
