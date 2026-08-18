package live

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/getcodescout/code_scout/internal/domain"
)

// paired mints a code, claims it as a device, and attaches a send plus a
// hang-up that records whether it ran.
func paired(t *testing.T, h *Hub, hungUp *int) uuid.UUID {
	t.Helper()

	projectID := uuid.New()
	minted, err := h.Mint(projectID, uuid.New())
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	s, err := h.Claim(projectID, minted.Code, domain.LiveDevice{DeviceModel: "Pixel 7"})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	h.AttachDevice(s.ID, func(any) error { return nil }, func() { *hungUp++ })
	return s.ID
}

// Ending a session has to hang up on the device, not merely stop writing to it.
//
// A device is usually healthy when a session is ended from the dashboard: it
// keeps answering pings, so its read deadline keeps being refreshed, and the
// socket handler goroutine and its ping ticker live for as long as the app
// does. Nothing else collects them, because every other path that reclaims a
// live session keys off the device having gone quiet.
func TestEndingASessionHangsUpOnTheDevice(t *testing.T) {
	h := NewHub()
	var hungUp int
	id := paired(t, h, &hungUp)

	h.End(id, "ended by the dashboard")

	if hungUp != 1 {
		t.Fatalf("hangUp ran %d times, want 1: the device's socket, handler "+
			"goroutine and ping ticker stay alive until the app closes", hungUp)
	}
}

// A session that runs out of time is torn down by the sweep rather than by End,
// and has to hang up for the same reason. This is the case nobody is watching.
func TestASweptSessionAlsoHangsUp(t *testing.T) {
	h := NewHub()
	var hungUp int
	paired(t, h, &hungUp)

	h.now = func() time.Time { return time.Now().Add(24 * time.Hour) }
	h.Sweep()

	if hungUp != 1 {
		t.Fatalf("hangUp ran %d times, want 1: a swept session left the device connected", hungUp)
	}
}

// End can race the sweep and both run the same teardown, so hanging up twice
// must be harmless.
func TestHangingUpIsIdempotent(t *testing.T) {
	h := NewHub()
	var hungUp int
	id := paired(t, h, &hungUp)

	h.End(id, "first")
	h.End(id, "second")
	h.Sweep()

	if hungUp != 1 {
		t.Fatalf("hangUp ran %d times, want exactly 1", hungUp)
	}
}
