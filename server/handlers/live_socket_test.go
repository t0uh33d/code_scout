package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// The database browser will write to a device socket from an HTTP handler,
// while the ping ticker is already writing to it from its own goroutine.
// gorilla/websocket permits one writer at a time and does not check for a
// second, so the two interleave and produce a corrupt frame rather than an
// error anyone could act on.
//
// Run under -race. Removing the mutex from deviceConn fails this with
// "WARNING: DATA RACE ... previous write at ... gorilla/websocket".
func TestDeviceConnSerialisesConcurrentWrites(t *testing.T) {
	dev, client, done := socketPair(t)
	defer done()

	// The client has to keep reading or the socket's buffer fills and every
	// writer blocks on the deadline instead of racing, which would hide the
	// very thing this is testing.
	go func() {
		for {
			if _, _, err := client.ReadMessage(); err != nil {
				return
			}
		}
	}()

	const writers, each = 8, 40

	var wg sync.WaitGroup
	errs := make(chan error, writers*each)

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < each; j++ {
				// Alternate the two write paths: a JSON frame is a multi-write
				// through NextWriter, a ping is a single control frame, and it
				// is the mix that shreds the stream when they overlap.
				var err error
				if j%2 == 0 {
					err = dev.writeJSON(map[string]any{
						"req": n, "seq": j, "pad": strings.Repeat("x", 256),
					})
				} else {
					err = dev.ping()
				}
				if err != nil {
					errs <- err
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("a write failed while others were in flight: %v", err)
	}
}

// A frame written while another is being written must still arrive whole. The
// race detector catches the memory race; this catches the consequence, so the
// test still means something when run without -race.
func TestDeviceConnFramesArriveIntact(t *testing.T) {
	dev, client, done := socketPair(t)
	defer done()

	const writers, each = 6, 25
	body := strings.Repeat("y", 1024)

	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < each; j++ {
				if err := dev.writeJSON(map[string]any{"w": n, "j": j, "pad": body}); err != nil {
					return
				}
			}
		}(i)
	}

	read := make(chan int, writers*each)
	go func() {
		defer close(read)
		for {
			client.SetReadDeadline(time.Now().Add(2 * time.Second))
			var frame struct {
				W   int    `json:"w"`
				J   int    `json:"j"`
				Pad string `json:"pad"`
			}
			// A torn frame fails to parse, or parses with a short pad. Both are
			// the corruption this exists to rule out.
			if err := client.ReadJSON(&frame); err != nil {
				return
			}
			if len(frame.Pad) != len(body) {
				read <- -1
				return
			}
			read <- frame.W
		}
	}()

	wg.Wait()

	got := 0
	for v := range read {
		if v == -1 {
			t.Fatal("a frame arrived truncated, so two writes interleaved")
		}
		got++
		if got == writers*each {
			break
		}
	}

	if got != writers*each {
		t.Fatalf("expected %d whole frames, got %d", writers*each, got)
	}
}

// Frames are told apart by which key they carry, not by their shape. The read
// loop routes on Req, so anything that changes how these parse sends answers to
// the log stream or log batches into the request matcher.
func TestDeviceFrameRouting(t *testing.T) {
	parse := func(raw string) deviceFrame {
		t.Helper()
		var f deviceFrame
		if err := json.Unmarshal([]byte(raw), &f); err != nil {
			t.Fatalf("parse %s: %v", raw, err)
		}
		return f
	}

	t.Run("a reply carries a request id", func(t *testing.T) {
		f := parse(`{"req":"abc123","ok":true,"rows":[]}`)
		if f.Req != "abc123" {
			t.Fatalf("req: got %q", f.Req)
		}
	})

	t.Run("a log batch never reads as a reply", func(t *testing.T) {
		// Every SDK sends this for a log batch, and none of them will ever put
		// a req on one. If it parsed with a non-empty Req the batch would go to
		// the request matcher, match nothing, and be dropped — a live stream
		// that goes blank with no error anywhere.
		f := parse(`{"logs":[{"message":"hello","level":"info"}]}`)
		if f.Req != "" {
			t.Fatalf("a log batch parsed with req %q", f.Req)
		}
		if len(f.Logs) != 1 {
			t.Fatalf("logs: got %d", len(f.Logs))
		}
	})

	t.Run("a heartbeat is neither", func(t *testing.T) {
		f := parse(`{"logs":[]}`)
		if f.Req != "" || len(f.Logs) != 0 {
			t.Fatalf("heartbeat parsed as req=%q logs=%d", f.Req, len(f.Logs))
		}
	})
}

// socketPair gives a deviceConn wrapping the server end of a real upgraded
// socket, and the client end to read from.
func socketPair(t *testing.T) (*deviceConn, *websocket.Conn, func()) {
	t.Helper()

	ready := make(chan *deviceConn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		ready <- &deviceConn{conn: conn}
		// Hold the handler open so the socket stays up for the test.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))

	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		srv.Close()
		t.Fatalf("dial: %v", err)
	}

	var dev *deviceConn
	select {
	case dev = <-ready:
	case <-time.After(3 * time.Second):
		client.Close()
		srv.Close()
		t.Fatal("the server never finished the upgrade")
	}

	return dev, client, func() {
		client.Close()
		srv.Close()
	}
}
