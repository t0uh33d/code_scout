package live

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/getcodescout/code_scout/internal/domain"
)

// The hub is the one piece of Code Scout that is pure concurrent state, so it
// is tested directly rather than through a handler. Run with -race.

func testHub(t *testing.T) (*Hub, *fakeClock) {
	t.Helper()
	clock := &fakeClock{at: time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)}
	h := NewHub()
	h.now = clock.Now
	return h, clock
}

type fakeClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

func device() domain.LiveDevice {
	return domain.LiveDevice{
		SessionID: "sess-1", InstallationID: "install-1",
		DeviceModel: "Pixel 7", OSName: "Android", OSVersion: "14",
	}
}

// drain collects what a watcher received, giving the publisher's goroutines a
// moment to deliver.
func drain(ch <-chan domain.LiveEvent) []domain.LiveEvent {
	var out []domain.LiveEvent
	deadline := time.After(time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
		case <-time.After(50 * time.Millisecond):
			return out
		case <-deadline:
			return out
		}
	}
}

func TestMintProducesATypeableCode(t *testing.T) {
	h, _ := testHub(t)

	s, err := h.Mint(uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if len(s.Code) != domain.PairingCodeLength {
		t.Errorf("code %q is %d characters, want %d", s.Code, len(s.Code), domain.PairingCodeLength)
	}
	if s.State != domain.LiveWaiting {
		t.Errorf("a fresh code should be waiting, got %q", s.State)
	}
	// The alphabet exists so nobody has to decide whether that is a zero or an
	// O while holding a phone.
	for _, r := range s.Code {
		if strings := "01ILOS58B"; containsRune(strings, r) {
			t.Errorf("code %q contains %q, which is ambiguous when read aloud", s.Code, r)
		}
	}
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}

func TestCodesAreUnique(t *testing.T) {
	h, _ := testHub(t)
	projectID := uuid.New()

	seen := make(map[string]bool)
	for i := 0; i < maxSessionsPerProject; i++ {
		s, err := h.Mint(projectID, uuid.New())
		if err != nil {
			t.Fatalf("mint %d: %v", i, err)
		}
		if seen[s.Code] {
			t.Fatalf("code %q minted twice: a second device would join the first one's session", s.Code)
		}
		seen[s.Code] = true
	}
}

// The property the whole pairing flow rests on. A code that works twice means
// a stranger reading a screen over someone's shoulder joins their session.
func TestACodeWorksOnce(t *testing.T) {
	h, _ := testHub(t)
	projectID := uuid.New()

	minted, _ := h.Mint(projectID, uuid.New())

	if _, err := h.Claim(projectID, minted.Code, device()); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if _, err := h.Claim(projectID, minted.Code, device()); err == nil {
		t.Fatal("the same code paired a second device")
	}
}

func TestClaimIsForgivingAboutTyping(t *testing.T) {
	h, _ := testHub(t)
	projectID := uuid.New()
	minted, _ := h.Mint(projectID, uuid.New())

	// Lower case with a space in the middle is what someone reading a code off
	// a screen actually types.
	typed := strings_ToLower(minted.Code[:3]) + " " + strings_ToLower(minted.Code[3:])
	if _, err := h.Claim(projectID, typed, device()); err != nil {
		t.Fatalf("claim %q for code %q: %v", typed, minted.Code, err)
	}
}

func strings_ToLower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

// A code minted for one project must not pair a device holding another
// project's credentials, or one team's app streams into another team's screen.
func TestACodeIsBoundToItsProject(t *testing.T) {
	h, _ := testHub(t)
	mine := uuid.New()
	theirs := uuid.New()

	minted, _ := h.Mint(mine, uuid.New())

	if _, err := h.Claim(theirs, minted.Code, device()); err == nil {
		t.Fatal("a device authenticated against another project claimed this code")
	}
	// And the code still works for the project it was minted for, so a probe
	// does not burn it.
	if _, err := h.Claim(mine, minted.Code, device()); err != nil {
		t.Fatalf("the rightful project can no longer use its own code: %v", err)
	}
}

func TestAnExpiredCodeIsRefused(t *testing.T) {
	h, clock := testHub(t)
	projectID := uuid.New()
	minted, _ := h.Mint(projectID, uuid.New())

	clock.advance(domain.PairingCodeTTL + time.Second)

	if _, err := h.Claim(projectID, minted.Code, device()); err == nil {
		t.Fatal("a code past its expiry still paired a device")
	}
	if got := h.List(projectID); len(got) != 0 {
		t.Errorf("an expired code is still listed as live: %+v", got)
	}
}

// Claiming stops the clock. A device that paired at 4:59 must not be cut off
// at 5:00 in the middle of a session.
func TestClaimingRemovesTheExpiry(t *testing.T) {
	h, clock := testHub(t)
	projectID := uuid.New()
	minted, _ := h.Mint(projectID, uuid.New())

	clock.advance(domain.PairingCodeTTL - time.Second)
	claimed, err := h.Claim(projectID, minted.Code, device())
	if err != nil {
		t.Fatalf("claim just before expiry: %v", err)
	}

	// Well past what would have been the expiry, but the device is talking.
	for range 5 {
		clock.advance(domain.LiveSessionIdleTimeout / 2)
		h.Touch(claimed.ID)
	}

	s, ok := h.Get(claimed.ID)
	if !ok || s.State != domain.LiveConnected {
		t.Fatalf("a live session was ended by its pairing expiry: %+v", s)
	}
}

func TestAQuietDeviceIsDroppedFromTheList(t *testing.T) {
	h, clock := testHub(t)
	projectID := uuid.New()
	minted, _ := h.Mint(projectID, uuid.New())
	claimed, _ := h.Claim(projectID, minted.Code, device())

	subEvents, err := h.Watch(claimed.ID, 0)
	events := subEvents.Events
	cancel := subEvents.Cancel
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	defer cancel()

	clock.advance(domain.LiveSessionIdleTimeout + time.Second)
	h.Sweep()

	if got := h.List(projectID); len(got) != 0 {
		t.Errorf("a device that stopped answering is still shown as live: %+v", got)
	}
	// And the watcher is told, rather than staring at a stream that simply
	// stopped producing.
	var ended bool
	for _, ev := range drain(events) {
		if ev.Kind == domain.LiveEventEnded {
			ended = true
			if ev.Reason == "" {
				t.Error("the ended event should say why")
			}
		}
	}
	if !ended {
		t.Error("the watcher was never told the session ended")
	}
}

func TestEveryWatcherGetsEveryEvent(t *testing.T) {
	h, _ := testHub(t)
	projectID := uuid.New()
	minted, _ := h.Mint(projectID, uuid.New())
	claimed, _ := h.Claim(projectID, minted.Code, device())

	subA, err := h.Watch(claimed.ID, 0)
	a := subA.Events
	cancelA := subA.Cancel
	if err != nil {
		t.Fatalf("watch a: %v", err)
	}
	defer cancelA()
	subB, err := h.Watch(claimed.ID, 0)
	b := subB.Events
	cancelB := subB.Cancel
	if err != nil {
		t.Fatalf("watch b: %v", err)
	}
	defer cancelB()

	h.Publish(claimed.ID, domain.LiveEventLog, []domain.LiveLog{
		{Message: "one"}, {Message: "two"}, {Message: "three"},
	})

	for name, ch := range map[string]<-chan domain.LiveEvent{"a": a, "b": b} {
		var messages []string
		for _, ev := range drain(ch) {
			if ev.Kind == domain.LiveEventLog && ev.Log != nil {
				messages = append(messages, ev.Log.Message)
			}
		}
		if len(messages) != 3 {
			t.Errorf("watcher %s got %v, want all three", name, messages)
		}
	}
}

// Sequence numbers are what let a watcher tell "nothing happened" from "I
// missed something".
func TestSequenceNumbersAreStrictlyIncreasing(t *testing.T) {
	h, _ := testHub(t)
	projectID := uuid.New()
	minted, _ := h.Mint(projectID, uuid.New())
	claimed, _ := h.Claim(projectID, minted.Code, device())

	subEvents, _ := h.Watch(claimed.ID, 0)
	events := subEvents.Events
	cancel := subEvents.Cancel
	defer cancel()

	h.Publish(claimed.ID, domain.LiveEventLog, []domain.LiveLog{{Message: "one"}, {Message: "two"}})
	h.Publish(claimed.ID, domain.LiveEventLog, []domain.LiveLog{{Message: "three"}})

	var last int64
	for _, ev := range drain(events) {
		if ev.Kind != domain.LiveEventLog {
			continue
		}
		if ev.Seq <= last {
			t.Fatalf("sequence went %d then %d", last, ev.Seq)
		}
		last = ev.Seq
	}
	if last != 3 {
		t.Errorf("last sequence = %d, want 3", last)
	}
}

// The load-bearing performance property. A watcher on a bad connection must
// never be able to slow down the phone that is publishing.
func TestASlowWatcherNeverBlocksThePublisher(t *testing.T) {
	h, _ := testHub(t)
	projectID := uuid.New()
	minted, _ := h.Mint(projectID, uuid.New())
	claimed, _ := h.Claim(projectID, minted.Code, device())

	// Subscribed and then never read from.
	subCancel, err := h.Watch(claimed.ID, 0)
	cancel := subCancel.Cancel
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < eventBuffer*4; i++ {
			h.Publish(claimed.ID, domain.LiveEventLog, []domain.LiveLog{{Message: "flood"}})
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("publishing blocked on a watcher that stopped reading")
	}
}

func TestEndingClosesEveryWatcher(t *testing.T) {
	h, _ := testHub(t)
	projectID := uuid.New()
	minted, _ := h.Mint(projectID, uuid.New())
	claimed, _ := h.Claim(projectID, minted.Code, device())

	subA, _ := h.Watch(claimed.ID, 0)
	a := subA.Events
	cancelA := subA.Cancel
	defer cancelA()
	subB, _ := h.Watch(claimed.ID, 0)
	b := subB.Events
	cancelB := subB.Cancel
	defer cancelB()

	h.End(claimed.ID, "ended from the dashboard")

	for name, ch := range map[string]<-chan domain.LiveEvent{"a": a, "b": b} {
		var sawEnded bool
		for _, ev := range drain(ch) {
			if ev.Kind == domain.LiveEventEnded {
				sawEnded = true
				if ev.Reason != "ended from the dashboard" {
					t.Errorf("watcher %s got reason %q", name, ev.Reason)
				}
			}
		}
		if !sawEnded {
			t.Errorf("watcher %s never received the ended event", name)
		}
		// The channel closes after the final event, which is how a watcher
		// knows to stop reading.
		if _, open := <-ch; open {
			t.Errorf("watcher %s channel is still open after the session ended", name)
		}
	}
}

// The device dropping and someone pressing End can land at the same moment.
func TestEndingTwiceIsSafe(t *testing.T) {
	h, _ := testHub(t)
	projectID := uuid.New()
	minted, _ := h.Mint(projectID, uuid.New())
	claimed, _ := h.Claim(projectID, minted.Code, device())

	subCancel, _ := h.Watch(claimed.ID, 0)
	cancel := subCancel.Cancel
	defer cancel()

	h.End(claimed.ID, "device left")
	h.End(claimed.ID, "ended from the dashboard")
}

// Unsubscribing after the session already ended must not close a closed
// channel. Both paths remove the watcher, and only one may close it.
func TestCancellingAfterTheSessionEndedIsSafe(t *testing.T) {
	h, _ := testHub(t)
	projectID := uuid.New()
	minted, _ := h.Mint(projectID, uuid.New())
	claimed, _ := h.Claim(projectID, minted.Code, device())

	subCancel, _ := h.Watch(claimed.ID, 0)
	cancel := subCancel.Cancel
	h.End(claimed.ID, "device left")
	cancel()
	cancel()
}

func TestWatcherCountReachesTheOtherWatchers(t *testing.T) {
	h, _ := testHub(t)
	projectID := uuid.New()
	minted, _ := h.Mint(projectID, uuid.New())
	claimed, _ := h.Claim(projectID, minted.Code, device())

	subFirst, _ := h.Watch(claimed.ID, 0)
	first := subFirst.Events
	cancelFirst := subFirst.Cancel
	defer cancelFirst()

	subCancelsecond, _ := h.Watch(claimed.ID, 0)
	cancelSecond := subCancelsecond.Cancel

	var latest int
	for _, ev := range drain(first) {
		if ev.Kind == domain.LiveEventWatcher {
			latest = ev.Watchers
		}
	}
	if latest != 2 {
		t.Errorf("the first watcher was told %d are watching, want 2", latest)
	}

	cancelSecond()
	latest = 0
	for _, ev := range drain(first) {
		if ev.Kind == domain.LiveEventWatcher {
			latest = ev.Watchers
		}
	}
	if latest != 1 {
		t.Errorf("after one left, the count says %d, want 1", latest)
	}
}

// Recovery. A watcher's connection drops for a few seconds — a laptop lid, a
// train tunnel — and EventSource reconnects on its own. Without a backlog the
// events in that window are simply gone, and nothing on screen says so.
func TestAReconnectingWatcherIsCaughtUp(t *testing.T) {
	h, _ := testHub(t)
	projectID := uuid.New()
	minted, _ := h.Mint(projectID, uuid.New())
	claimed, _ := h.Claim(projectID, minted.Code, device())

	first, err := h.Watch(claimed.ID, 0)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}

	h.Publish(claimed.ID, domain.LiveEventLog, []domain.LiveLog{{Message: "seen"}})

	var lastSeen int64
	for _, ev := range drain(first.Events) {
		if ev.Kind == domain.LiveEventLog {
			lastSeen = ev.Seq
		}
	}
	if lastSeen == 0 {
		t.Fatal("the first watcher saw nothing to reconnect from")
	}

	// The connection drops, and the app keeps going without anyone watching.
	first.Cancel()
	h.Publish(claimed.ID, domain.LiveEventLog, []domain.LiveLog{{Message: "missed one"}})
	h.Publish(claimed.ID, domain.LiveEventLog, []domain.LiveLog{{Message: "missed two"}})

	// EventSource comes back with the last id it saw.
	again, err := h.Watch(claimed.ID, lastSeen)
	if err != nil {
		t.Fatalf("re-watch: %v", err)
	}
	defer again.Cancel()

	var recovered []string
	for _, ev := range again.Backlog {
		if ev.Kind == domain.LiveEventLog && ev.Log != nil {
			recovered = append(recovered, ev.Log.Message)
		}
	}
	if len(recovered) != 2 || recovered[0] != "missed one" || recovered[1] != "missed two" {
		t.Errorf("recovered %v, want both missed messages in order", recovered)
	}
	if again.Missed != 0 {
		t.Errorf("Missed = %d, want 0: everything was still in the ring", again.Missed)
	}
	// And nothing already seen comes back a second time.
	for _, ev := range again.Backlog {
		if ev.Seq <= lastSeen {
			t.Errorf("event %d was replayed although it had already been delivered", ev.Seq)
		}
	}
}

// A watcher opening a live stream for the first time wants to see what happens
// next, not a replay of the last ten minutes.
func TestAFirstTimeWatcherGetsNoBacklog(t *testing.T) {
	h, _ := testHub(t)
	projectID := uuid.New()
	minted, _ := h.Mint(projectID, uuid.New())
	claimed, _ := h.Claim(projectID, minted.Code, device())

	for range 5 {
		h.Publish(claimed.ID, domain.LiveEventLog, []domain.LiveLog{{Message: "before anyone watched"}})
	}

	sub, err := h.Watch(claimed.ID, 0)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	defer sub.Cancel()

	if len(sub.Backlog) != 0 {
		t.Errorf("a first-time watcher was given %d events of history", len(sub.Backlog))
	}
	if sub.Missed != 0 {
		t.Errorf("Missed = %d, want 0: nothing was missed, there was nothing to miss", sub.Missed)
	}
}

// Away for longer than the ring holds. The events are genuinely gone, and the
// watcher has to be told rather than shown a timeline with a silent hole.
func TestAWatcherAwayTooLongIsToldWhatItMissed(t *testing.T) {
	h, _ := testHub(t)
	projectID := uuid.New()
	minted, _ := h.Mint(projectID, uuid.New())
	claimed, _ := h.Claim(projectID, minted.Code, device())

	h.Publish(claimed.ID, domain.LiveEventLog, []domain.LiveLog{{Message: "one"}})
	lastSeen := int64(1)

	// Comfortably more than the ring can hold.
	for range recentBuffer + 50 {
		h.Publish(claimed.ID, domain.LiveEventLog, []domain.LiveLog{{Message: "flood"}})
	}

	sub, err := h.Watch(claimed.ID, lastSeen)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	defer sub.Cancel()

	if sub.Missed <= 0 {
		t.Error("a gap larger than the ring was reported as no gap at all")
	}
	if len(sub.Backlog) != recentBuffer {
		t.Errorf("backlog is %d events, want the whole ring (%d)", len(sub.Backlog), recentBuffer)
	}
	// What is returned is still the most recent, not the oldest.
	if len(sub.Backlog) > 0 {
		newest := sub.Backlog[len(sub.Backlog)-1].Seq
		if newest != int64(recentBuffer+51) {
			t.Errorf("newest replayed event is %d, want the latest published", newest)
		}
	}
}

// The ring must not grow without bound on a device left streaming over lunch.
func TestTheReplayRingIsBounded(t *testing.T) {
	h, _ := testHub(t)
	projectID := uuid.New()
	minted, _ := h.Mint(projectID, uuid.New())
	claimed, _ := h.Claim(projectID, minted.Code, device())

	for range recentBuffer * 3 {
		h.Publish(claimed.ID, domain.LiveEventLog, []domain.LiveLog{{Message: "tick"}})
	}

	h.mu.RLock()
	held := len(h.sessions[claimed.ID].recent)
	h.mu.RUnlock()

	if held > recentBuffer {
		t.Errorf("the replay ring holds %d events, want at most %d", held, recentBuffer)
	}
}

func TestWatchingAnUnknownSessionFails(t *testing.T) {
	h, _ := testHub(t)
	if _, err := h.Watch(uuid.New(), 0); err == nil {
		t.Fatal("watching a session that never existed should fail")
	}
}

func TestListIsScopedToItsProject(t *testing.T) {
	h, _ := testHub(t)
	mine := uuid.New()
	theirs := uuid.New()

	h.Mint(mine, uuid.New())
	h.Mint(theirs, uuid.New())
	h.Mint(theirs, uuid.New())

	if got := len(h.List(mine)); got != 1 {
		t.Errorf("my project lists %d sessions, want 1", got)
	}
	if got := len(h.List(theirs)); got != 2 {
		t.Errorf("their project lists %d sessions, want 2", got)
	}
}

func TestListIsNewestFirst(t *testing.T) {
	h, clock := testHub(t)
	projectID := uuid.New()

	first, _ := h.Mint(projectID, uuid.New())
	clock.advance(time.Second)
	second, _ := h.Mint(projectID, uuid.New())

	got := h.List(projectID)
	if len(got) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(got))
	}
	if got[0].ID != second.ID || got[1].ID != first.ID {
		t.Error("the code someone just minted should be at the top of their screen")
	}
}

func TestProjectSessionsAreBounded(t *testing.T) {
	h, _ := testHub(t)
	projectID := uuid.New()

	for i := 0; i < maxSessionsPerProject; i++ {
		if _, err := h.Mint(projectID, uuid.New()); err != nil {
			t.Fatalf("mint %d: %v", i, err)
		}
	}
	if _, err := h.Mint(projectID, uuid.New()); err == nil {
		t.Fatal("a project minted past its session cap")
	}
	// Another project is unaffected: the cap is per project, not global.
	if _, err := h.Mint(uuid.New(), uuid.New()); err != nil {
		t.Errorf("one project at its cap blocked another: %v", err)
	}
}

func TestWatchersAreBounded(t *testing.T) {
	h, _ := testHub(t)
	projectID := uuid.New()
	minted, _ := h.Mint(projectID, uuid.New())
	claimed, _ := h.Claim(projectID, minted.Code, device())

	for i := range maxWatchersPerSession {
		sub, err := h.Watch(claimed.ID, 0)
		if err != nil {
			t.Fatalf("watcher %d: %v", i, err)
		}
		defer sub.Cancel()
	}
	if _, err := h.Watch(claimed.ID, 0); err == nil {
		t.Fatal("watchers grew past the cap")
	}
}

func TestClosingEndsEverySession(t *testing.T) {
	h, _ := testHub(t)
	projectID := uuid.New()
	minted, _ := h.Mint(projectID, uuid.New())
	claimed, _ := h.Claim(projectID, minted.Code, device())

	subEvents, _ := h.Watch(claimed.ID, 0)
	events := subEvents.Events
	cancel := subEvents.Cancel
	defer cancel()

	h.Close()

	var sawEnded bool
	for _, ev := range drain(events) {
		if ev.Kind == domain.LiveEventEnded {
			sawEnded = true
		}
	}
	if !sawEnded {
		t.Error("a restarting server left its watchers hanging")
	}
}

// Everything above runs one goroutine at a time. This runs the real shape:
// a device publishing while watchers come and go. Its job is to fail under
// -race, not to assert an outcome.
func TestHubUnderConcurrentUse(t *testing.T) {
	h, _ := testHub(t)
	projectID := uuid.New()
	minted, _ := h.Mint(projectID, uuid.New())
	claimed, _ := h.Claim(projectID, minted.Code, device())

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				h.Publish(claimed.ID, domain.LiveEventLog, []domain.LiveLog{{Message: "tick"}})
			}
		}
	}()

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				subCh, err := h.Watch(claimed.ID, 0)
				ch := subCh.Events
				cancel := subCh.Cancel
				if err != nil {
					continue
				}
				select {
				case <-ch:
				default:
				}
				cancel()
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 50; j++ {
			h.List(projectID)
			h.Get(claimed.ID)
			h.Sweep()
		}
	}()

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()

	h.End(claimed.ID, "test over")
}
