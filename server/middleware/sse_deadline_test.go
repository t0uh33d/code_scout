package middleware

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The SSE handlers clear the server's write deadline so a stream can stay open
// past WriteTimeout. That only works if http.ResponseController can find the
// real ResponseWriter underneath the logging wrapper, which is what Unwrap is
// for.
//
// Asserting on the error from SetWriteDeadline rather than on the presence of
// the method: the method existing proves nothing if a future wrapper is added
// between this one and the connection, and the failure mode is silent either
// way. "feature not supported" is what a missing Unwrap actually produces.
func TestSSECanClearItsWriteDeadline(t *testing.T) {
	got := make(chan error, 1)

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- http.NewResponseController(w).SetWriteDeadline(time.Time{})
	})

	srv := httptest.NewServer(HttpLogger(h))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/stream/logs?project_id=x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if err := <-got; err != nil {
		t.Fatalf("SetWriteDeadline through HttpLogger: %v\n"+
			"the logging wrapper is hiding the real ResponseWriter, so every SSE "+
			"stream will be cut off at the server's WriteTimeout", err)
	}
}

// The bug this guards was not that the deadline could not be cleared, but what
// that cost: a stream severed mid-flight while the handler carried on writing.
// This drives a real one and fails if frames stop arriving before the handler
// says it is done.
func TestAStreamOutlivesTheWriteTimeout(t *testing.T) {
	const frames = 6

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := http.NewResponseController(w).SetWriteDeadline(time.Time{}); err != nil {
			t.Errorf("SetWriteDeadline: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("no flusher through the logging wrapper")
			return
		}
		for i := range frames {
			fmt.Fprintf(w, "event: log\ndata: %d\n\n", i)
			flusher.Flush()
			time.Sleep(120 * time.Millisecond)
		}
	})

	srv := httptest.NewUnstartedServer(HttpLogger(h))
	// Far shorter than the 6 frames take to send, so an uncleared deadline is
	// certain to cut the stream rather than merely likely to.
	srv.Config.WriteTimeout = 250 * time.Millisecond
	srv.Start()
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/stream/logs?project_id=x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	read := 0
	br := bufio.NewReader(resp.Body)
	for read < frames {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("stream ended after %d of %d frames: %v\n"+
				"the write deadline was not cleared", read, frames, err)
		}
		if len(line) > 6 && line[:6] == "data: " {
			read++
		}
	}
}
