// Package live holds the in-memory registry behind live devices and streaming.
//
// Nothing here touches the database. A live session is a socket, a handful of
// facts about the phone on the other end, and a fan-out to whoever is watching
// — all of which stop being true the moment the process does. Persisting it
// would mean writing rows that are wrong immediately after a restart.
//
// That does bound the deployment: live sessions are per node, so a multi-replica
// install needs a shared publisher before this works across replicas. Single
// node is the documented default and the roadmap's plan for going wider is a
// Redis-backed publisher, which slots in behind this same interface.
package live

import (
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/getcodescout/code_scout/internal/domain"
)

var (
	// ErrNoSuchCode covers a code that never existed, one already claimed, and
	// one that expired. Deliberately one error: telling a caller which of the
	// three it was would let someone probe for live codes.
	ErrNoSuchCode = errors.New("that code is not valid")

	ErrNoSuchSession = errors.New("no such live session")

	ErrTooManyWatchers = errors.New("too many watchers on this session")

	ErrTooManySessions = errors.New("too many live sessions for this project")
)

const (
	// maxWatchersPerSession is a fan-out bound, not a licence. Several people
	// watching one device is a real case; fifty is a leak.
	maxWatchersPerSession = 20

	// maxSessionsPerProject bounds what one project can hold open at once,
	// including codes still waiting to be claimed.
	maxSessionsPerProject = 20

	// eventBuffer is per watcher. A watcher that falls this far behind loses
	// events rather than slowing the device down — a live tail that stalls the
	// app it is tailing would be worse than a gap.
	eventBuffer = 256

	// recentBuffer is what a reconnecting watcher can be caught up on. A phone
	// on a train drops its watcher's connection for a few seconds at a time,
	// and without this the events in that window are simply gone — the stream
	// resumes and nothing says anything was missed.
	//
	// Sized for a busy app across a short blip rather than for a long absence:
	// past this the watcher is told how many it missed instead of being quietly
	// shown an incomplete timeline.
	recentBuffer = 512
)

// Subscription is one watcher's view of a session: what it missed, and what
// happens next.
type Subscription struct {
	// Backlog is what arrived while this watcher was away, oldest first.
	Backlog []domain.LiveEvent

	// Missed counts events that fell out of the ring before the watcher came
	// back. Non-zero means the timeline has a hole in it, and saying so is the
	// whole reason this is reported rather than swallowed.
	Missed int64

	// Events carries everything from here on. Closed when the session ends.
	Events <-chan domain.LiveEvent

	// Cancel unsubscribes. Safe to call more than once.
	Cancel func()
}

type watcher struct {
	ch chan domain.LiveEvent
}

type session struct {
	meta     domain.LiveSession
	seq      int64
	watchers map[*watcher]struct{}

	// recent is the replay ring, oldest first, holding at most recentBuffer
	// events. dropped counts what has already fallen off the front, which is
	// how a watcher learns its gap is bigger than the ring.
	recent  []domain.LiveEvent
	dropped int64
}

// remember appends to the replay ring, discarding the oldest once it is full.
func (s *session) remember(ev domain.LiveEvent) {
	s.recent = append(s.recent, ev)
	if len(s.recent) > recentBuffer {
		s.dropped += int64(len(s.recent) - recentBuffer)
		s.recent = s.recent[len(s.recent)-recentBuffer:]
	}
}

// since returns the events after seq, and how many are missing because they
// had already fallen out of the ring.
//
// seq 0 means a watcher that has never seen anything: it gets no backlog at
// all, because arriving at a live stream should show what happens next, not
// replay the last five minutes.
func (s *session) since(seq int64) ([]domain.LiveEvent, int64) {
	if seq <= 0 {
		return nil, 0
	}

	out := make([]domain.LiveEvent, 0, len(s.recent))
	for _, ev := range s.recent {
		if ev.Seq > seq {
			out = append(out, ev)
		}
	}

	// The oldest event still held. Anything between the watcher's last seen
	// sequence and that one is gone for good.
	var missed int64
	if len(s.recent) > 0 {
		if oldest := s.recent[0].Seq; oldest > seq+1 {
			missed = oldest - seq - 1
		}
	}
	return out, missed
}

// Hub owns every live session on this node.
type Hub struct {
	mu       sync.RWMutex
	sessions map[uuid.UUID]*session
	byCode   map[string]uuid.UUID

	// now is swappable so the tests can age a session without sleeping.
	now func() time.Time
}

func NewHub() *Hub {
	return &Hub{
		sessions: make(map[uuid.UUID]*session),
		byCode:   make(map[string]uuid.UUID),
		now:      time.Now,
	}
}

// Mint creates a waiting session and returns its pairing code.
func (h *Hub) Mint(projectID, createdBy uuid.UUID) (domain.LiveSession, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := h.now()
	h.sweepLocked(now)

	live := 0
	for _, s := range h.sessions {
		if s.meta.ProjectID == projectID && s.meta.State != domain.LiveEnded {
			live++
		}
	}
	if live >= maxSessionsPerProject {
		return domain.LiveSession{}, ErrTooManySessions
	}

	// Retry rather than trusting one draw. A collision is vanishingly unlikely
	// and catastrophic if it happens: the second device would join the first
	// one's session and stream someone else's app into their screen.
	var code string
	for attempt := 0; ; attempt++ {
		c, err := domain.NewPairingCode()
		if err != nil {
			return domain.LiveSession{}, err
		}
		if _, taken := h.byCode[c]; !taken {
			code = c
			break
		}
		if attempt == 8 {
			return domain.LiveSession{}, errors.New("could not mint an unused pairing code")
		}
	}

	s := &session{
		meta: domain.LiveSession{
			ID:        uuid.New(),
			ProjectID: projectID,
			Code:      code,
			State:     domain.LiveWaiting,
			CreatedBy: createdBy,
			CreatedAt: now,
			ExpiresAt: now.Add(domain.PairingCodeTTL),
		},
		watchers: make(map[*watcher]struct{}),
	}
	h.sessions[s.meta.ID] = s
	h.byCode[code] = s.meta.ID
	return s.meta, nil
}

// Claim is the device's half of pairing. It takes the code out of circulation
// on success, so a second device typing the same code gets ErrNoSuchCode.
func (h *Hub) Claim(projectID uuid.UUID, code string, device domain.LiveDevice) (domain.LiveSession, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := h.now()
	h.sweepLocked(now)

	id, ok := h.byCode[domain.NormalisePairingCode(code)]
	if !ok {
		return domain.LiveSession{}, ErrNoSuchCode
	}
	s, ok := h.sessions[id]
	if !ok || s.meta.State != domain.LiveWaiting {
		return domain.LiveSession{}, ErrNoSuchCode
	}
	// A code minted for one project must not pair a device authenticated
	// against another, or one project's credentials would stream into another
	// project's dashboard.
	if s.meta.ProjectID != projectID {
		return domain.LiveSession{}, ErrNoSuchCode
	}

	// Single use. Removed here rather than on end, so a device that connects
	// and immediately drops cannot have its code reused by anyone watching the
	// screen it was displayed on.
	delete(h.byCode, s.meta.Code)

	s.meta.State = domain.LiveConnected
	s.meta.Device = device
	s.meta.ClaimedAt = now
	s.meta.LastSeen = now
	return s.meta, nil
}

// Publish fans an event out to every watcher and marks the device alive.
//
// Never blocks on a watcher. A full buffer means that watcher is too slow, and
// the right answer is to drop its events rather than to hold up the phone.
//
// The sends happen while the lock is still held, which looks heavy-handed and
// is not: every send is non-blocking, so the critical section is a bounded walk
// over the watcher set. Releasing the lock first and sending to a snapshot is
// what an earlier version did, and it panicked — a watcher can unsubscribe in
// that gap, and unsubscribing closes the channel being sent to.
func (h *Hub) Publish(sessionID uuid.UUID, kind domain.LiveEventKind, logs []domain.LiveLog) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s, ok := h.sessions[sessionID]
	if !ok || s.meta.State == domain.LiveEnded {
		return
	}
	now := h.now()
	s.meta.LastSeen = now

	events := make([]domain.LiveEvent, 0, max(len(logs), 1))
	if len(logs) == 0 {
		s.seq++
		events = append(events, domain.LiveEvent{Kind: kind, At: now, Seq: s.seq})
	}
	for i := range logs {
		s.seq++
		events = append(events, domain.LiveEvent{
			Kind: kind, At: now, Seq: s.seq, Log: &logs[i],
		})
	}

	for _, ev := range events {
		s.remember(ev)
	}

	for w := range s.watchers {
		for _, ev := range events {
			select {
			case w.ch <- ev:
			default:
			}
		}
	}
}

// Touch records that the device is still there, without publishing anything.
// The socket's pong handler calls this.
func (h *Hub) Touch(sessionID uuid.UUID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s, ok := h.sessions[sessionID]; ok {
		s.meta.LastSeen = h.now()
	}
}

// Watch subscribes a dashboard to a session, catching it up from afterSeq
// first. Pass 0 for a watcher that has never seen anything.
//
// The backlog and the subscription are taken under the same lock, deliberately.
// Reading the recent events and then subscribing would drop anything published
// in between; subscribing and then reading them would deliver it twice. Doing
// both at once is the only ordering with neither hole nor duplicate, and it is
// the entire reason this returns a struct rather than being two calls.
//
// The returned channel is closed when the session ends, which is how a watcher
// learns the stream is over without needing a heartbeat of its own.
func (h *Hub) Watch(sessionID uuid.UUID, afterSeq int64) (*Subscription, error) {
	h.mu.Lock()

	s, ok := h.sessions[sessionID]
	if !ok || s.meta.State == domain.LiveEnded {
		h.mu.Unlock()
		return nil, ErrNoSuchSession
	}
	if len(s.watchers) >= maxWatchersPerSession {
		h.mu.Unlock()
		return nil, ErrTooManyWatchers
	}

	backlog, missed := s.since(afterSeq)

	w := &watcher{ch: make(chan domain.LiveEvent, eventBuffer)}
	s.watchers[w] = struct{}{}
	s.meta.Watchers = len(s.watchers)
	count := s.meta.Watchers
	h.mu.Unlock()

	h.announceWatchers(sessionID, count)

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			s, ok := h.sessions[sessionID]
			if !ok {
				h.mu.Unlock()
				return
			}
			if _, present := s.watchers[w]; !present {
				// Already removed by End, which closed the channel too.
				h.mu.Unlock()
				return
			}
			delete(s.watchers, w)
			s.meta.Watchers = len(s.watchers)
			left := s.meta.Watchers
			close(w.ch)
			h.mu.Unlock()
			h.announceWatchers(sessionID, left)
		})
	}
	return &Subscription{
		Backlog: backlog,
		Missed:  missed,
		Events:  w.ch,
		Cancel:  cancel,
	}, nil
}

// announceWatchers tells the remaining watchers the count changed, so the
// "2 watching" label is right without anyone polling for it.
// The read lock is held across the sends for the same reason Publish holds the
// write lock: a channel may only be closed under the write lock, so sending
// outside any lock races an unsubscribe.
func (h *Hub) announceWatchers(sessionID uuid.UUID, count int) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	s, ok := h.sessions[sessionID]
	if !ok {
		return
	}

	ev := domain.LiveEvent{Kind: domain.LiveEventWatcher, At: h.now(), Watchers: count}
	for w := range s.watchers {
		select {
		case w.ch <- ev:
		default:
		}
	}
}

// End stops a session and releases everyone watching it. Idempotent: ending an
// already-ended session is not an error, because the device dropping and
// someone pressing End can happen at the same moment.
func (h *Hub) End(sessionID uuid.UUID, reason string) {
	h.mu.Lock()
	s, ok := h.sessions[sessionID]
	if !ok || s.meta.State == domain.LiveEnded {
		h.mu.Unlock()
		return
	}
	h.endLocked(s, reason)
	h.mu.Unlock()
}

// endLocked marks a session over, tells its watchers why, and closes them out.
// The final event is sent before the channel closes so a watcher can render
// "the device left" rather than just going silent.
func (h *Hub) endLocked(s *session, reason string) {
	now := h.now()
	s.meta.State = domain.LiveEnded
	s.meta.EndedAt = now
	s.meta.Watchers = 0
	delete(h.byCode, s.meta.Code)

	s.seq++
	final := domain.LiveEvent{Kind: domain.LiveEventEnded, At: now, Seq: s.seq, Reason: reason}
	for w := range s.watchers {
		select {
		case w.ch <- final:
		default:
		}
		close(w.ch)
		delete(s.watchers, w)
	}
}

// Get returns one session, whatever state it is in.
func (h *Hub) Get(sessionID uuid.UUID) (domain.LiveSession, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sweepLocked(h.now())

	s, ok := h.sessions[sessionID]
	if !ok {
		return domain.LiveSession{}, false
	}
	return s.meta, true
}

// List returns a project's sessions that are still worth showing: waiting for a
// device, or streaming. Newest first, so a code someone just minted is at the
// top of the screen they minted it from.
func (h *Hub) List(projectID uuid.UUID) []domain.LiveSession {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sweepLocked(h.now())

	out := make([]domain.LiveSession, 0, len(h.sessions))
	for _, s := range h.sessions {
		if s.meta.ProjectID == projectID && s.meta.State != domain.LiveEnded {
			out = append(out, s.meta)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].CreatedAt.After(out[j-1].CreatedAt); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Sweep ends whatever has run out of time and forgets what ended a while ago.
// Called on every read as well as from the cron, so a list is never stale even
// if the ticker is late.
func (h *Hub) Sweep() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sweepLocked(h.now())
}

func (h *Hub) sweepLocked(now time.Time) {
	for id, s := range h.sessions {
		switch {
		case s.meta.Expired(now):
			h.endLocked(s, "the code expired before a device used it")
		case s.meta.Idle(now):
			h.endLocked(s, "the device went quiet")
		case s.meta.State == domain.LiveEnded && now.Sub(s.meta.EndedAt) > time.Minute:
			// Kept for a minute after ending so a watcher that reconnects in
			// that window is told the session ended rather than that it never
			// existed.
			delete(h.sessions, id)
		}
	}
}

// Close ends every session. Called on shutdown, so a restarting server tells
// its watchers rather than letting their connections hang.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.sessions {
		if s.meta.State != domain.LiveEnded {
			h.endLocked(s, "the server is restarting")
		}
	}
}
