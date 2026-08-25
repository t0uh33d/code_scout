package sse

import (
	"testing"
	"time"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/google/uuid"
)

func testLog(msg string) domain.Log {
	return domain.Log{
		ID:      uuid.New(),
		Level:   "info",
		Message: msg,
	}
}

func TestSubscribePublish(t *testing.T) {
	b := NewBroker()
	defer b.Close()

	pid := uuid.New()
	ch, err := b.Subscribe(pid)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	b.Publish(pid, []domain.Log{testLog("hello")})

	select {
	case received := <-ch:
		if len(received) != 1 || received[0].Message != "hello" {
			t.Fatalf("unexpected: %+v", received)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestUnsubscribe(t *testing.T) {
	b := NewBroker()
	defer b.Close()

	pid := uuid.New()
	ch, _ := b.Subscribe(pid)
	b.Unsubscribe(pid, ch)

	_, ok := <-ch
	if ok {
		t.Fatal("expected closed channel")
	}
	if b.SubscriberCount(pid) != 0 {
		t.Fatal("expected 0 subscribers")
	}
}

func TestNonBlockingSend(t *testing.T) {
	b := NewBroker()
	b.bufferSize = 2
	defer b.Close()

	pid := uuid.New()
	b.Subscribe(pid)

	// Fill buffer
	b.Publish(pid, []domain.Log{testLog("1")})
	b.Publish(pid, []domain.Log{testLog("2")})

	// Should not block
	done := make(chan struct{})
	go func() {
		b.Publish(pid, []domain.Log{testLog("3")})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked")
	}
}

func TestMaxSubscribers(t *testing.T) {
	b := NewBroker()
	b.maxPerProject = 3
	defer b.Close()

	pid := uuid.New()
	for i := 0; i < 3; i++ {
		if _, err := b.Subscribe(pid); err != nil {
			t.Fatalf("Subscribe %d failed: %v", i, err)
		}
	}

	_, err := b.Subscribe(pid)
	if err == nil {
		t.Fatal("expected error when exceeding max")
	}
}

func TestClose(t *testing.T) {
	b := NewBroker()

	ch1, _ := b.Subscribe(uuid.New())
	ch2, _ := b.Subscribe(uuid.New())

	b.Close()

	if _, ok := <-ch1; ok {
		t.Fatal("ch1 should be closed")
	}
	if _, ok := <-ch2; ok {
		t.Fatal("ch2 should be closed")
	}
}

func TestPublishNoSubscribers(t *testing.T) {
	b := NewBroker()
	defer b.Close()
	// Should not panic
	b.Publish(uuid.New(), []domain.Log{testLog("nobody")})
}
