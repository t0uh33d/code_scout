package sse

import (
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
)

// Broker is an in-memory pub/sub that fans out log events to SSE subscribers by project ID.
type Broker struct {
	mu            sync.RWMutex
	subscribers   map[uuid.UUID]map[chan []domain.Log]struct{}
	maxPerProject int
	bufferSize    int
}

// NewBroker creates a broker with sensible defaults.
func NewBroker() *Broker {
	return &Broker{
		subscribers:   make(map[uuid.UUID]map[chan []domain.Log]struct{}),
		maxPerProject: 50,
		bufferSize:    100,
	}
}

// Subscribe creates a buffered channel for a project and registers it.
func (b *Broker) Subscribe(projectID uuid.UUID) (chan []domain.Log, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	subs, ok := b.subscribers[projectID]
	if !ok {
		subs = make(map[chan []domain.Log]struct{})
		b.subscribers[projectID] = subs
	}

	if len(subs) >= b.maxPerProject {
		return nil, fmt.Errorf("max subscribers (%d) reached for project %s", b.maxPerProject, projectID)
	}

	ch := make(chan []domain.Log, b.bufferSize)
	subs[ch] = struct{}{}
	return ch, nil
}

// Unsubscribe removes and closes a subscriber channel.
func (b *Broker) Unsubscribe(projectID uuid.UUID, ch chan []domain.Log) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if subs, ok := b.subscribers[projectID]; ok {
		if _, exists := subs[ch]; exists {
			delete(subs, ch)
			close(ch)
		}
		if len(subs) == 0 {
			delete(b.subscribers, projectID)
		}
	}
}

// Publish sends logs to all subscribers for a project. Non-blocking: if a channel buffer
// is full, the event is dropped for that subscriber (avoids blocking the publisher).
func (b *Broker) Publish(projectID uuid.UUID, logs []domain.Log) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	subs, ok := b.subscribers[projectID]
	if !ok {
		return
	}

	for ch := range subs {
		select {
		case ch <- logs:
		default:
			// Buffer full — drop this event for this slow subscriber
		}
	}
}

// Close closes all subscriber channels and clears the map. Call during server shutdown.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for projectID, subs := range b.subscribers {
		for ch := range subs {
			close(ch)
		}
		delete(b.subscribers, projectID)
	}
}

// SubscriberCount returns the number of active subscribers for a project.
func (b *Broker) SubscriberCount(projectID uuid.UUID) int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return len(b.subscribers[projectID])
}
