package live

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/getcodescout/code_scout/internal/domain"
)

// connected mints a session, claims it, and attaches a device whose sender runs
// reply for every frame it is given. Pass nil for a device that takes questions
// and never answers.
func connected(t *testing.T, h *Hub, reply func(req deviceRequest)) uuid.UUID {
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

	h.AttachDevice(s.ID, func(frame any) error {
		req, ok := frame.(deviceRequest)
		if !ok {
			t.Errorf("the device was sent %T, not a deviceRequest", frame)
			return nil
		}
		if reply != nil {
			reply(req)
		}
		return nil
	}, nil)
	return s.ID
}

func TestAskReturnsTheDeviceAnswer(t *testing.T) {
	h := NewHub()

	var sessionID uuid.UUID
	sessionID = connected(t, h, func(req deviceRequest) {
		// Answering from inside the send call is the tightest possible race
		// between registering the pending entry and the reply arriving. If Ask
		// registered after writing, this would deadlock until the timeout.
		h.Deliver(sessionID, req.Req, json.RawMessage(`{"rows":[{"id":1}]}`))
	})

	got, err := h.Ask(context.Background(), sessionID, DeviceCommand{
		Op:   "rows",
		Args: map[string]any{"table": "users"},
	})
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if string(got) != `{"rows":[{"id":1}]}` {
		t.Fatalf("got %s", got)
	}
}

func TestAskSendsTheOpAndArgsItWasGiven(t *testing.T) {
	h := NewHub()

	var seen deviceRequest
	var sessionID uuid.UUID
	sessionID = connected(t, h, func(req deviceRequest) {
		seen = req
		h.Deliver(sessionID, req.Req, json.RawMessage(`{}`))
	})

	if _, err := h.Ask(context.Background(), sessionID, DeviceCommand{
		Op:   "rows",
		Args: map[string]any{"table": "orders", "limit": 100},
	}); err != nil {
		t.Fatalf("ask: %v", err)
	}

	if seen.Op != "rows" {
		t.Errorf("op: got %q", seen.Op)
	}
	if seen.Args["table"] != "orders" {
		t.Errorf("args: got %v", seen.Args)
	}
	if seen.Req == "" {
		t.Error("the frame carried no request id, so no answer could ever be matched to it")
	}
}

func TestAskTimesOutOnASilentDevice(t *testing.T) {
	h := NewHub()
	sessionID := connected(t, h, nil) // takes the question, never answers

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := h.Ask(ctx, sessionID, DeviceCommand{Op: "schema"})
	if !errors.Is(err, ErrDeviceSilent) {
		t.Fatalf("want ErrDeviceSilent, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("waited %v, so the caller's own deadline was ignored", elapsed)
	}
}

func TestAskFailsFastWhenTheSessionEndsMidQuestion(t *testing.T) {
	h := NewHub()
	sessionID := connected(t, h, nil)

	go func() {
		time.Sleep(30 * time.Millisecond)
		h.End(sessionID, "the device left")
	}()

	start := time.Now()
	_, err := h.Ask(context.Background(), sessionID, DeviceCommand{Op: "schema"})
	if !errors.Is(err, ErrDeviceGone) {
		t.Fatalf("want ErrDeviceGone, got %v", err)
	}
	// The point of closing the pending channels. Without it this waits out the
	// full deviceAskTimeout for a device that is already gone, and somebody
	// watches a spinner for ten seconds.
	if elapsed := time.Since(start); elapsed >= deviceAskTimeout {
		t.Fatalf("waited %v for a session that ended, so the timeout answered instead of End", elapsed)
	}
}

func TestAskWithNoDeviceAttached(t *testing.T) {
	h := NewHub()
	projectID := uuid.New()
	minted, err := h.Mint(projectID, uuid.New())
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	// Minted but never claimed: a code on a screen with no phone behind it.
	_, err = h.Ask(context.Background(), minted.ID, DeviceCommand{Op: "schema"})
	if !errors.Is(err, ErrDeviceGone) {
		t.Fatalf("want ErrDeviceGone, got %v", err)
	}
}

func TestAskOnAnUnknownSession(t *testing.T) {
	h := NewHub()
	_, err := h.Ask(context.Background(), uuid.New(), DeviceCommand{Op: "schema"})
	if !errors.Is(err, ErrNoSuchSession) {
		t.Fatalf("want ErrNoSuchSession, got %v", err)
	}
}

func TestAskWhenTheWriteFails(t *testing.T) {
	h := NewHub()
	projectID := uuid.New()
	minted, _ := h.Mint(projectID, uuid.New())
	s, _ := h.Claim(projectID, minted.Code, domain.LiveDevice{})

	h.AttachDevice(s.ID, func(any) error { return errors.New("broken pipe") }, nil)

	// A socket that refuses the write is a device that has gone, and it must
	// not leave the caller waiting out the timeout for an answer that can never
	// come.
	start := time.Now()
	_, err := h.Ask(context.Background(), s.ID, DeviceCommand{Op: "schema"})
	if !errors.Is(err, ErrDeviceGone) {
		t.Fatalf("want ErrDeviceGone, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("a failed write took %v to report", elapsed)
	}
}

func TestDeliverIgnoresAnswersNobodyAskedFor(t *testing.T) {
	h := NewHub()
	sessionID := connected(t, h, nil)

	// A reply to a question that already timed out, and an id the device
	// invented. Neither should panic or block.
	h.Deliver(sessionID, "never-asked", json.RawMessage(`{}`))
	h.Deliver(sessionID, "", json.RawMessage(`{}`))
	h.Deliver(uuid.New(), "anything", json.RawMessage(`{}`))
}

func TestDeliverTwiceForOneRequestIsHarmless(t *testing.T) {
	h := NewHub()

	var sessionID uuid.UUID
	sessionID = connected(t, h, func(req deviceRequest) {
		h.Deliver(sessionID, req.Req, json.RawMessage(`{"n":1}`))
		h.Deliver(sessionID, req.Req, json.RawMessage(`{"n":2}`))
	})

	got, err := h.Ask(context.Background(), sessionID, DeviceCommand{Op: "schema"})
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	// The first answer wins. A device that answers twice is confused, and
	// picking the later one would mean a slow duplicate could overwrite a
	// correct reply that had already been read.
	if string(got) != `{"n":1}` {
		t.Fatalf("got %s, want the first answer", got)
	}
}

func TestAskLeavesNothingPendingBehind(t *testing.T) {
	h := NewHub()

	var sessionID uuid.UUID
	sessionID = connected(t, h, func(req deviceRequest) {
		h.Deliver(sessionID, req.Req, json.RawMessage(`{}`))
	})

	for i := 0; i < 20; i++ {
		if _, err := h.Ask(context.Background(), sessionID, DeviceCommand{Op: "schema"}); err != nil {
			t.Fatalf("ask %d: %v", i, err)
		}
	}
	// A timed-out question has to clean up after itself too, or every failed
	// read leaks a map entry and a channel for the life of the session.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	silent := NewHub()
	silentID := connected(t, silent, nil)
	silent.Ask(ctx, silentID, DeviceCommand{Op: "schema"})

	h.mu.RLock()
	left := len(h.sessions[sessionID].pending)
	h.mu.RUnlock()
	if left != 0 {
		t.Fatalf("%d pending entries left behind", left)
	}

	silent.mu.RLock()
	leftSilent := len(silent.sessions[silentID].pending)
	silent.mu.RUnlock()
	if leftSilent != 0 {
		t.Fatalf("%d pending entries left behind after a timeout", leftSilent)
	}
}

func TestConcurrentAsksEachGetTheirOwnAnswer(t *testing.T) {
	h := NewHub()

	// Every question is answered with its own request id echoed back, so a
	// crossed wire shows up as a mismatch rather than as a hang.
	var sessionID uuid.UUID
	sessionID = connected(t, h, func(req deviceRequest) {
		go func() {
			payload, _ := json.Marshal(map[string]string{"echo": req.Args["mine"].(string)})
			h.Deliver(sessionID, req.Req, payload)
		}()
	})

	const askers = 24
	var wg sync.WaitGroup
	errs := make(chan error, askers)

	for i := 0; i < askers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			mine := uuid.NewString()
			got, err := h.Ask(context.Background(), sessionID, DeviceCommand{
				Op:   "rows",
				Args: map[string]any{"mine": mine},
			})
			if err != nil {
				errs <- err
				return
			}
			var out struct {
				Echo string `json:"echo"`
			}
			json.Unmarshal(got, &out)
			if out.Echo != mine {
				errs <- errors.New("an answer went to the wrong asker")
			}
		}(i)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
