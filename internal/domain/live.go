package domain

import (
	"crypto/rand"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
)

// A live session is one device streaming to one project, in real time, for as
// long as the app stays in the foreground. None of it is stored: a live session
// exists in memory on the node holding the device's socket and is gone when the
// process restarts. That is deliberate — the prototype's rule is that streamed
// events are ephemeral unless someone turns on Persist.

// pairingAlphabet leaves out the characters people mistype when reading a code
// off a screen and into a phone: 0/O, 1/I/L, 5/S, 8/B. What is left is
// unambiguous in every font we ship.
const pairingAlphabet = "234679ACDEFGHJKMNPQRTUVWXYZ"

// PairingCodeLength is short enough to type one-handed. Six characters of this
// alphabet is about 387 million combinations, and a code lives for five
// minutes, is single use, and only ever grants access to one project — so
// guessing is not the attack worth defending against here.
const PairingCodeLength = 6

// PairingCodeTTL is how long a minted code waits for a device. Long enough to
// walk to the test phone, short enough that a code left on a screen at lunch
// is dead by the time anyone walks past it.
const PairingCodeTTL = 5 * time.Minute

// LiveSessionIdleTimeout ends a session whose device has gone quiet. A phone
// that is backgrounded or killed does not always close its socket, so without
// this the Live devices list fills up with devices that are not there.
const LiveSessionIdleTimeout = 45 * time.Second

// NewPairingCode mints a code from crypto/rand. math/rand would be predictable
// from one observed code, and a predictable code is one an outsider can claim
// before the person who asked for it does.
func NewPairingCode() (string, error) {
	limit := big.NewInt(int64(len(pairingAlphabet)))
	var b strings.Builder
	for range PairingCodeLength {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", err
		}
		b.WriteByte(pairingAlphabet[n.Int64()])
	}
	return b.String(), nil
}

// NormalisePairingCode is what a typed code goes through before it is compared.
// People type lower case and read a space into the middle of a six-character
// code; neither should be a failed pairing.
func NormalisePairingCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(code) {
		if strings.ContainsRune(pairingAlphabet, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// LiveSessionState is what the Live devices screen renders per row.
type LiveSessionState string

const (
	// LiveWaiting is a minted code nobody has claimed yet.
	LiveWaiting LiveSessionState = "waiting"
	// LiveConnected is a device streaming right now.
	LiveConnected LiveSessionState = "connected"
	// LiveEnded is over, by any route: the device left, someone ended it from
	// the dashboard, or the code expired unclaimed.
	LiveEnded LiveSessionState = "ended"
)

// LiveDevice is what a device says about itself when it claims a code. It
// mirrors the session record the SDK already builds for uploads, so the live
// list and the Sessions screen describe a phone the same way.
type LiveDevice struct {
	SessionID      string `json:"session_id"`
	InstallationID string `json:"installation_id"`
	DeviceModel    string `json:"device_model"`
	OSName         string `json:"os_name"`
	OSVersion      string `json:"os_version"`
	AppVersion     string `json:"app_version"`
	BuildNumber    string `json:"build_number"`
	UserID         string `json:"user_id"`
}

// Label is what the dashboard puts on the row: "Pixel 7 · Android 14".
func (d LiveDevice) Label() string {
	model := d.DeviceModel
	if model == "" {
		model = "Unknown device"
	}
	if d.OSName == "" {
		return model
	}
	os := d.OSName
	if d.OSVersion != "" {
		os += " " + d.OSVersion
	}
	return model + " · " + os
}

// LiveSession is one pairing and the stream that follows it.
type LiveSession struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Code      string
	State     LiveSessionState

	// CreatedBy is who minted the code. Only they see it before it is claimed,
	// so two people setting up sessions at once cannot read each other's.
	CreatedBy uuid.UUID

	Device LiveDevice

	CreatedAt time.Time
	ExpiresAt time.Time
	ClaimedAt time.Time
	LastSeen  time.Time
	EndedAt   time.Time

	// Watchers is how many dashboards have this session open.
	Watchers int
}

// Expired reports whether an unclaimed code has run out of time. A claimed
// session has no expiry — it runs until the device leaves.
func (s *LiveSession) Expired(now time.Time) bool {
	return s.State == LiveWaiting && now.After(s.ExpiresAt)
}

// Idle reports whether a connected device has stopped saying anything. The
// socket's own ping keeps LastSeen moving, so this only fires when the phone is
// genuinely gone rather than merely quiet.
func (s *LiveSession) Idle(now time.Time) bool {
	return s.State == LiveConnected && now.Sub(s.LastSeen) > LiveSessionIdleTimeout
}

// ExpiresIn is what the waiting card counts down. Never negative, so a template
// cannot render "-0:03".
func (s *LiveSession) ExpiresIn(now time.Time) time.Duration {
	if d := s.ExpiresAt.Sub(now); d > 0 {
		return d
	}
	return 0
}

// LiveEventKind distinguishes the things that travel down a stream.
type LiveEventKind string

const (
	LiveEventLog     LiveEventKind = "log"
	LiveEventEnded   LiveEventKind = "ended"
	LiveEventDevice  LiveEventKind = "device"
	LiveEventWatcher LiveEventKind = "watcher"
)

// LiveLog is one log as it travels a live session, in both directions: up from
// the device and back out to every watcher.
//
// Its own type rather than domain.Log, which carries no JSON tags because
// nothing was ever meant to serialise it — it is the database entity, with a
// client id, a project id and a fingerprint that mean nothing to a phone or a
// browser. Reusing it here sent watchers `Message` while the page read
// `message`, and every row arrived blank.
type LiveLog struct {
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`

	// Network calls render as one line each rather than as a message, the same
	// way they do in the log viewer.
	IsNetworkCall bool   `json:"is_network_call,omitempty"`
	RequestID     string `json:"request_id,omitempty"`
	CallPhase     string `json:"call_phase,omitempty"`
	Method        string `json:"method,omitempty"`
	URL           string `json:"url,omitempty"`
	StatusCode    int    `json:"status_code,omitempty"`

	// Headers and bodies, for the live network inspector. The SDK sends it on
	// network calls only, already redacted at capture, and it passes through
	// here untouched: the browser pairs the phases and renders it, the same
	// division of labour as the rest of the live screen.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// LiveEvent is one thing that happened, on its way to every watcher.
type LiveEvent struct {
	Kind LiveEventKind `json:"kind"`
	At   time.Time     `json:"at"`

	// Seq is per session and strictly increasing, so a watcher can tell a
	// dropped event from a quiet app.
	Seq int64 `json:"seq"`

	Log *LiveLog `json:"log,omitempty"`

	// Reason accompanies an ended event: "device left", "ended from the
	// dashboard", "device went quiet".
	Reason string `json:"reason,omitempty"`

	// Watchers accompanies a watcher event.
	Watchers int `json:"watchers,omitempty"`
}
