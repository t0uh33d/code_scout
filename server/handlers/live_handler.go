package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/live"
	"github.com/getcodescout/code_scout/internal/ports"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"github.com/getcodescout/code_scout/server/middleware"
	"github.com/getcodescout/code_scout/view"
)

// Two transports, because the two ends do different things.
//
// The device pushes, continuously, from a mobile network, and needs the server
// to be able to hang up — so it gets a WebSocket, which is also what the SDK's
// dart:io already speaks.
//
// A watcher only ever receives, so it gets Server-Sent Events: the same
// mechanism the log viewer's live tail already uses here, with reconnection and
// proxy behaviour the browser handles rather than us.

const (
	// deviceWriteWait bounds a write to a phone that has stopped reading.
	deviceWriteWait = 10 * time.Second

	// devicePingEvery keeps the socket alive through NATs and tells us the
	// phone is still there. It has to be comfortably under the idle timeout, or
	// a perfectly healthy quiet app would be swept.
	devicePingEvery = 15 * time.Second

	// deviceReadWait is how long we wait to hear anything at all, pongs
	// included. Longer than two pings, so one lost pong is not a disconnect.
	deviceReadWait = 40 * time.Second

	// maxDeviceMessage bounds one frame from a device. A live batch is a
	// handful of logs; anything this size is a bug or an attack.
	maxDeviceMessage = 1 << 20
)

type LiveHandler struct {
	hub        *live.Hub
	projectSvc ports.ProjectManager
}

func NewLiveHandler(hub *live.Hub, projectSvc ports.ProjectManager) *LiveHandler {
	return &LiveHandler{hub: hub, projectSvc: projectSvc}
}

// projectFromRequest loads the project named in the path. Access has already
// been checked by the subrouter's middleware; this is only the load.
func (h *LiveHandler) projectFromRequest(w http.ResponseWriter, r *http.Request) *domain.Project {
	projectID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return nil
	}
	project, status, err := h.projectSvc.GetProject(r.Context(), projectID)
	if err != nil {
		http.Error(w, "Project not found", status)
		return nil
	}
	return project
}

// upgrader rejects cross-origin upgrades by default. The device is not a
// browser and sends no Origin at all, which CheckOrigin has to allow — a
// missing Origin is not a cross-site request, it is a native client.
var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return r.Header.Get("Origin") == ""
	},
}

// LiveDevices renders the Live devices screen.
func (h *LiveHandler) LiveDevices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	project := h.projectFromRequest(w, r)
	if project == nil {
		return
	}

	actor := middleware.UserFrom(ctx)
	data := view.LiveDevicesData{
		Project:  project,
		User:     actor,
		Sessions: h.visibleSessions(project.ID, actorID(actor)),
		Now:      time.Now(),
	}

	if r.Header.Get("HX-Request") == "true" {
		view.LiveDevicesBody(data).Render(ctx, w)
		return
	}
	view.LiveDevicesPage(data).Render(ctx, w)
}

// LiveDevicesList is what the screen polls. It is a fragment rather than a
// stream because it changes on the scale of someone picking up a phone, and a
// four-line poll beats a second streaming endpoint to maintain.
func (h *LiveHandler) LiveDevicesList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	project := h.projectFromRequest(w, r)
	if project == nil {
		return
	}

	actor := middleware.UserFrom(ctx)
	view.LiveDevicesList(view.LiveDevicesData{
		Project:  project,
		User:     actor,
		Sessions: h.visibleSessions(project.ID, actorID(actor)),
		Now:      time.Now(),
	}).Render(ctx, w)
}

// visibleSessions hides other people's unclaimed codes. A code on screen is a
// credential until a device takes it, so only the person who minted it sees it;
// once a device is connected the session is the project's and everyone sees it.
func (h *LiveHandler) visibleSessions(projectID, actor uuid.UUID) []domain.LiveSession {
	all := h.hub.List(projectID)
	out := make([]domain.LiveSession, 0, len(all))
	for _, s := range all {
		if s.State == domain.LiveWaiting && s.CreatedBy != actor {
			continue
		}
		out = append(out, s)
	}
	return out
}

// NewLiveSession mints a pairing code.
func (h *LiveHandler) NewLiveSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	project := h.projectFromRequest(w, r)
	if project == nil {
		return
	}
	actor := middleware.UserFrom(ctx)

	if _, err := h.hub.Mint(project.ID, actorID(actor)); err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to mint a pairing code")
		// 200 with the message in the markup: htmx does not swap the body of a
		// non-2xx response, so an error rendered with 429 would never appear.
		view.LiveDevicesList(view.LiveDevicesData{
			Project: project, User: actor, Now: time.Now(),
			Sessions: h.visibleSessions(project.ID, actorID(actor)),
			Error:    liveErrorMessage(err),
		}).Render(ctx, w)
		return
	}

	view.LiveDevicesList(view.LiveDevicesData{
		Project: project, User: actor, Now: time.Now(),
		Sessions: h.visibleSessions(project.ID, actorID(actor)),
	}).Render(ctx, w)
}

func liveErrorMessage(err error) string {
	switch {
	case errors.Is(err, live.ErrTooManySessions):
		return "This project already has as many live sessions as it can hold. End one first."
	default:
		return "Could not start a live session. Try again."
	}
}

// EndLiveSession stops a session from the dashboard.
func (h *LiveHandler) EndLiveSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	project := h.projectFromRequest(w, r)
	if project == nil {
		return
	}

	sessionID, err := uuid.Parse(mux.Vars(r)["sid"])
	if err != nil {
		http.Error(w, "Invalid session", http.StatusBadRequest)
		return
	}
	// Scoped to the project in the URL, which the access middleware has already
	// checked. Without this, knowing a session id would be enough to end
	// someone else's stream.
	if s, ok := h.hub.Get(sessionID); !ok || s.ProjectID != project.ID {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	h.hub.End(sessionID, "ended from the dashboard")

	actor := middleware.UserFrom(ctx)
	view.LiveDevicesList(view.LiveDevicesData{
		Project: project, User: actor, Now: time.Now(),
		Sessions: h.visibleSessions(project.ID, actorID(actor)),
	}).Render(ctx, w)
}

// LiveStream renders the screen that watches one device.
func (h *LiveHandler) LiveStream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	project := h.projectFromRequest(w, r)
	if project == nil {
		return
	}

	sessionID, err := uuid.Parse(mux.Vars(r)["sid"])
	if err != nil {
		http.Error(w, "Invalid session", http.StatusBadRequest)
		return
	}

	// A session that has ended still renders, saying so. Redirecting instead
	// would drop someone who came back to a tab a minute later onto a list with
	// no explanation of where their device went.
	session, found := h.hub.Get(sessionID)
	if found && session.ProjectID != project.ID {
		found = false
	}

	data := view.LiveStreamData{
		Project: project,
		User:    middleware.UserFrom(ctx),
		Session: session,
		Found:   found,
	}

	if r.Header.Get("HX-Request") == "true" {
		view.LiveStreamBody(data).Render(ctx, w)
		return
	}
	view.LiveStreamPage(data).Render(ctx, w)
}

// WatchStream is the SSE endpoint the stream screen reads.
func (h *LiveHandler) WatchStream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := cslog.L(ctx)

	project := h.projectFromRequest(w, r)
	if project == nil {
		return
	}

	sessionID, err := uuid.Parse(mux.Vars(r)["sid"])
	if err != nil {
		http.Error(w, "Invalid session", http.StatusBadRequest)
		return
	}
	if s, ok := h.hub.Get(sessionID); !ok || s.ProjectID != project.ID {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	// The server's write timeout would otherwise cut a healthy stream off at
	// 30 seconds.
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		log.WithError(err).Warn("Live: could not clear the write deadline")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// EventSource resends the last id it saw as Last-Event-ID when it
	// reconnects, which it does on its own after a dropped connection. Reading
	// it here is what turns a blip into a gap the server can fill rather than
	// events nobody ever learns were missed.
	sub, err := h.hub.Watch(sessionID, lastEventID(r))
	if err != nil {
		if errors.Is(err, live.ErrTooManyWatchers) {
			http.Error(w, "Too many people are watching this device", http.StatusTooManyRequests)
			return
		}
		http.Error(w, "That session is over", http.StatusNotFound)
		return
	}
	defer sub.Cancel()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "event: connected\ndata: {\"session_id\":\"%s\",\"missed\":%d}\n\n",
		sessionID, sub.Missed)
	flusher.Flush()

	// The backlog first, in order, before anything live. A watcher that came
	// back must see what it missed in the place it happened rather than
	// interleaved with what is arriving now.
	for _, ev := range sub.Backlog {
		if !writeEvent(w, flusher, ev) {
			return
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case ev, open := <-sub.Events:
			if !open {
				// The hub closed us out, which only happens when the session
				// ended. The ended event arrived just before this.
				return
			}
			if !writeEvent(w, flusher, ev) {
				return
			}
		}
	}
}

// writeEvent sends one event, id included, and reports whether it worked.
//
// The id line is what makes recovery possible at all: the browser remembers the
// last one it saw and hands it back on reconnect.
func writeEvent(w http.ResponseWriter, flusher http.Flusher, ev domain.LiveEvent) bool {
	payload, err := json.Marshal(ev)
	if err != nil {
		// Skip the one event rather than dropping the connection: a single
		// unencodable log is not a reason to end somebody's session.
		return true
	}
	if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.Seq, ev.Kind, payload); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

// lastEventID reads where a reconnecting watcher got to. The header is what
// EventSource sends by itself; the query parameter is for anything driving this
// endpoint by hand, and for the tests.
//
// Anything unparseable reads as "start from now", which shows a live stream
// rather than replaying a backlog against a client that cannot place it.
func lastEventID(r *http.Request) int64 {
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		raw = r.URL.Query().Get("last_event_id")
	}
	seq, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || seq < 0 {
		return 0
	}
	return seq
}

// deviceHello is the first frame a device sends, carrying the code it was given
// and what it is.
type deviceHello struct {
	Code   string            `json:"code"`
	Device domain.LiveDevice `json:"device"`
}

// deviceFrame is every frame after the hello.
type deviceFrame struct {
	Logs []domain.LiveLog `json:"logs"`
}

// DeviceSocket is the device's end. It runs behind the SDK credential
// middleware, so the project is already known and authenticated before a code
// is ever looked at — the code decides *which session*, never *whether you may*.
func (h *LiveHandler) DeviceSocket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := cslog.L(ctx)

	project, err := middleware.GetProjectFromContext(ctx)
	if err != nil {
		RespondError(w, err)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade has already written its own response.
		log.WithError(err).Debug("Live: upgrade refused")
		return
	}
	defer conn.Close()

	conn.SetReadLimit(maxDeviceMessage)
	conn.SetReadDeadline(time.Now().Add(deviceReadWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(deviceReadWait))
	})

	var hello deviceHello
	if err := conn.ReadJSON(&hello); err != nil {
		writeSocketError(conn, "expected a pairing message")
		return
	}

	session, err := h.hub.Claim(project.ID, hello.Code, hello.Device)
	if err != nil {
		// One message for every failure. Telling a device that a code exists
		// but is already claimed would turn this into a way to test codes.
		writeSocketError(conn, "that code is not valid")
		return
	}

	log.WithField("project_id", project.ID).
		WithField("live_session", session.ID).
		Info("Live: device paired")

	// Tell the device it is live, so the overlay can move off its pairing
	// screen only once the server agrees.
	conn.SetWriteDeadline(time.Now().Add(deviceWriteWait))
	if err := conn.WriteJSON(map[string]any{
		"ok": true, "session_id": session.ID.String(),
	}); err != nil {
		h.hub.End(session.ID, "the device left")
		return
	}

	// Pings run on their own goroutine. Writes are not safe from two
	// goroutines at once, so this one owns every write from here on and the
	// read loop below never writes.
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(devicePingEvery)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				conn.SetWriteDeadline(time.Now().Add(deviceWriteWait))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}()

	defer func() {
		close(done)
		h.hub.End(session.ID, "the device left")
		log.WithField("live_session", session.ID).Info("Live: device gone")
	}()

	for {
		var frame deviceFrame
		if err := conn.ReadJSON(&frame); err != nil {
			return
		}
		// An empty frame is a heartbeat from the app rather than nothing at
		// all: it says the phone is awake even when the user is reading.
		if len(frame.Logs) == 0 {
			h.hub.Touch(session.ID)
			continue
		}
		h.hub.Publish(session.ID, domain.LiveEventLog, frame.Logs)
	}
}

// writeSocketError says why before hanging up, so the overlay can show
// something better than "disconnected".
func writeSocketError(conn *websocket.Conn, reason string) {
	conn.SetWriteDeadline(time.Now().Add(deviceWriteWait))
	conn.WriteJSON(map[string]any{"ok": false, "error": reason})
}
