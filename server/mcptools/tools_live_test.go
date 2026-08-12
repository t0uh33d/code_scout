package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/live"
	"github.com/google/uuid"
)

// liveHubWith mints and claims one connected session on a real hub, so the
// tail tests exercise the actual ring, watcher accounting and sweep.
func liveHubWith(t *testing.T, projectID uuid.UUID) (*live.Hub, domain.LiveSession) {
	t.Helper()
	hub := live.NewHub()
	t.Cleanup(hub.Close)

	minted, err := hub.Mint(projectID, uuid.New())
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	claimed, err := hub.Claim(projectID, minted.Code, domain.LiveDevice{DeviceModel: "Pixel 7", OSName: "Android", OSVersion: "14"})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	return hub, claimed
}

func publishLogs(hub *live.Hub, sid uuid.UUID, n int) {
	for i := range n {
		hub.Publish(sid, domain.LiveEventLog, []domain.LiveLog{{Level: "info", Message: fmt.Sprintf("log %d", i)}})
	}
}

func TestTailCancelsItsWatcherImmediately(t *testing.T) {
	projectID := uuid.New()
	hub, session_ := liveHubWith(t, projectID)
	publishLogs(hub, session_.ID, 5)

	cs := session(t, Deps{
		Logs:   &fakeLogs{},
		Access: fakeAccess{readable: map[uuid.UUID]bool{projectID: true}},
		Live:   hub,
	})

	res := call(t, cs, "tail_live_session", map[string]any{
		"project_id": projectID.String(), "live_session_id": session_.ID.String(),
	})
	if res.IsError {
		t.Fatalf("tail failed: %s", resultText(res))
	}
	if !strings.Contains(resultText(res), "log 4") {
		t.Errorf("the tail should carry the buffered logs: %s", resultText(res))
	}

	after, _ := hub.Get(session_.ID)
	if after.Watchers != 0 {
		t.Errorf("the tool left %d watcher(s) behind; a one-shot read must cancel", after.Watchers)
	}
}

func TestTailReportsMissedEventsHonestly(t *testing.T) {
	projectID := uuid.New()
	hub, session_ := liveHubWith(t, projectID)
	// Overflow the 512-event ring so the oldest fall off.
	publishLogs(hub, session_.ID, 600)

	cs := session(t, Deps{
		Logs:   &fakeLogs{},
		Access: fakeAccess{readable: map[uuid.UUID]bool{projectID: true}},
		Live:   hub,
	})

	res := call(t, cs, "tail_live_session", map[string]any{
		"project_id": projectID.String(), "live_session_id": session_.ID.String(), "max_events": 10,
	})
	if res.IsError {
		t.Fatalf("tail failed: %s", resultText(res))
	}
	var out struct {
		Missed    int64 `json:"missed"`
		Dropped   int   `json:"dropped"`
		LatestSeq int64 `json:"latest_seq"`
		Events    []struct {
			Seq int64 `json:"seq"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(resultText(res)), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Missed == 0 {
		t.Error("600 events through a 512 ring should report a hole, not a tidy timeline")
	}
	if len(out.Events) != 10 || out.Dropped == 0 {
		t.Errorf("max_events should cap the tail and say what it cut: %d events, dropped %d", len(out.Events), out.Dropped)
	}
	if out.LatestSeq != out.Events[len(out.Events)-1].Seq {
		t.Error("latest_seq should be the newest returned event's seq")
	}
}

func TestALiveSessionOfAnotherProjectReadsAsNotFound(t *testing.T) {
	mine, theirs := uuid.New(), uuid.New()
	hub, session_ := liveHubWith(t, theirs) // lives under the OTHER project

	cs := session(t, Deps{
		Logs: &fakeLogs{},
		// The caller can read their own project; the session is not in it.
		Access: fakeAccess{readable: map[uuid.UUID]bool{mine: true}},
		Live:   hub,
	})

	res := call(t, cs, "tail_live_session", map[string]any{
		"project_id": mine.String(), "live_session_id": session_.ID.String(),
	})
	if !res.IsError || !strings.Contains(resultText(res), "not found") {
		t.Errorf("another project's live session must read as absent, got: %s", resultText(res))
	}
}

func TestThePairingCodeNeverAppearsInTheLiveList(t *testing.T) {
	projectID := uuid.New()
	hub := live.NewHub()
	t.Cleanup(hub.Close)
	minted, err := hub.Mint(projectID, uuid.New())
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	cs := session(t, Deps{
		Logs:   &fakeLogs{},
		Access: fakeAccess{readable: map[uuid.UUID]bool{projectID: true}},
		Live:   hub,
	})

	res := call(t, cs, "list_live_sessions", map[string]any{"project_id": projectID.String()})
	if res.IsError {
		t.Fatalf("list failed: %s", resultText(res))
	}
	text := resultText(res)
	if !strings.Contains(text, minted.ID.String()) {
		t.Fatal("the waiting session should be listed")
	}
	// An unclaimed pairing code is a credential: whoever types it becomes
	// this project's device.
	if strings.Contains(text, minted.Code) {
		t.Errorf("the pairing code crossed the wire: %s", text)
	}
}

// fakeAskHub records the command Ask was given and answers with a canned
// reply, for pinning exactly what the tools send a device.
type fakeAskHub struct {
	session domain.LiveSession
	got     *live.DeviceCommand
	reply   json.RawMessage
	err     error
}

func (f *fakeAskHub) List(uuid.UUID) []domain.LiveSession { return nil }
func (f *fakeAskHub) Get(uuid.UUID) (domain.LiveSession, bool) {
	return f.session, true
}
func (f *fakeAskHub) Watch(uuid.UUID, int64) (*live.Subscription, error) {
	return nil, live.ErrNoSuchSession
}
func (f *fakeAskHub) Ask(_ context.Context, _ uuid.UUID, cmd live.DeviceCommand) (json.RawMessage, error) {
	f.got = &cmd
	if f.err != nil {
		return nil, f.err
	}
	return f.reply, nil
}

func TestRowsAlwaysSendsTheServerLimit(t *testing.T) {
	projectID := uuid.New()
	sid := uuid.New()
	hub := &fakeAskHub{
		session: domain.LiveSession{ID: sid, ProjectID: projectID, State: domain.LiveConnected},
		reply:   json.RawMessage(`{"ok":true,"page":{"columns":[],"rows":[],"handles":[],"has_more":false,"row_handle":"rowid","kind":"sql"}}`),
	}
	cs := session(t, Deps{
		Logs:   &fakeLogs{},
		Access: fakeAccess{readable: map[uuid.UUID]bool{projectID: true}},
		Live:   hub,
	})

	res := call(t, cs, "live_db_rows", map[string]any{
		"project_id": projectID.String(), "live_session_id": sid.String(),
		"db": "shop.db", "namespace": "cart", "offset": 100,
		"filters": map[string]string{"user_id": "42"},
	})
	if res.IsError {
		t.Fatalf("rows failed: %s", resultText(res))
	}

	if hub.got == nil {
		t.Fatal("the device was never asked")
	}
	if hub.got.Op != "rows" {
		t.Errorf("op = %q, want the hardcoded literal rows", hub.got.Op)
	}
	if limit, ok := hub.got.Args["limit"].(int); !ok || limit != liveDBRowsLimit {
		t.Errorf("limit sent to the device = %v, must always be the server constant %d", hub.got.Args["limit"], liveDBRowsLimit)
	}
	if hub.got.Args["offset"] != 100 {
		t.Errorf("offset did not pass through: %v", hub.got.Args["offset"])
	}
}

func TestASilentDeviceSaysOpenTheApp(t *testing.T) {
	projectID := uuid.New()
	sid := uuid.New()
	hub := &fakeAskHub{
		session: domain.LiveSession{ID: sid, ProjectID: projectID, State: domain.LiveConnected},
		err:     live.ErrDeviceSilent,
	}
	cs := session(t, Deps{
		Logs:   &fakeLogs{},
		Access: fakeAccess{readable: map[uuid.UUID]bool{projectID: true}},
		Live:   hub,
	})

	res := call(t, cs, "live_db_sources", map[string]any{
		"project_id": projectID.String(), "live_session_id": sid.String(),
	})
	if !res.IsError {
		t.Fatal("a silent device did not error")
	}
	if !strings.Contains(resultText(res), "open the app on the phone") {
		t.Errorf("the message should tell the person what to do: %s", resultText(res))
	}
}

// A device refusal is the device's words, passed through; ours add nothing.
func TestADeviceRefusalIsPassedThrough(t *testing.T) {
	projectID := uuid.New()
	sid := uuid.New()
	hub := &fakeAskHub{
		session: domain.LiveSession{ID: sid, ProjectID: projectID, State: domain.LiveConnected},
		reply:   json.RawMessage(`{"ok":false,"error":"Unknown database \"shop2.db\"."}`),
	}
	cs := session(t, Deps{
		Logs:   &fakeLogs{},
		Access: fakeAccess{readable: map[uuid.UUID]bool{projectID: true}},
		Live:   hub,
	})

	res := call(t, cs, "live_db_namespaces", map[string]any{
		"project_id": projectID.String(), "live_session_id": sid.String(), "db": "shop2.db",
	})
	if !res.IsError || !strings.Contains(resultText(res), `Unknown database "shop2.db"`) {
		t.Errorf("the device's own words should reach the caller: %s", resultText(res))
	}
}
