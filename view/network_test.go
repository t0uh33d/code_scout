package view

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/getcodescout/code_scout/internal/domain"
)

func call(start time.Time, d time.Duration, hasResponse bool) domain.NetworkCall {
	return domain.NetworkCall{
		RequestID:   uuid.New(),
		StartedAt:   start,
		EndedAt:     start.Add(d),
		HasRequest:  true,
		HasResponse: hasResponse,
	}
}

// The waterfall's whole job is showing that a slow call and a late call are
// different things. If the maths is wrong the bars still render, just lying.
func TestWaterfallBarPlacesCallsInTheWindow(t *testing.T) {
	base := time.Now()
	span := 10 * time.Second

	// Starts at the window's start, runs a tenth of it.
	offset, width := waterfallBar(call(base, time.Second, true), base, span)
	if offset != 0 {
		t.Errorf("a call at the start should sit at 0%%, got %.1f", offset)
	}
	if width < 9 || width > 11 {
		t.Errorf("a 1s call in a 10s window should be about 10%% wide, got %.1f", width)
	}

	// Starts halfway.
	offset, _ = waterfallBar(call(base.Add(5*time.Second), time.Second, true), base, span)
	if offset < 49 || offset > 51 {
		t.Errorf("a call 5s into a 10s window should sit near 50%%, got %.1f", offset)
	}
}

// A fast call in a long window rounds to nothing, which would read as "this
// call did not happen".
func TestWaterfallBarStaysVisible(t *testing.T) {
	base := time.Now()
	_, width := waterfallBar(call(base, time.Millisecond, true), base, time.Hour)
	if width < 2 {
		t.Errorf("a bar must stay visible, got %.2f%%", width)
	}
}

// A pending call runs to the end of the window: it has not finished, and a stub
// would read as one that finished instantly.
func TestWaterfallBarRunsPendingToTheEnd(t *testing.T) {
	base := time.Now()
	pending := domain.NetworkCall{StartedAt: base, EndedAt: base, HasRequest: true}

	offset, width := waterfallBar(pending, base, 10*time.Second)
	if offset+width < 99 {
		t.Errorf("a pending bar should reach the end of the window, got %.1f + %.1f", offset, width)
	}
}

// Nothing may overflow its track, whatever the timestamps say. A device clock
// running ahead is enough to produce an end after the window.
func TestWaterfallBarNeverOverflows(t *testing.T) {
	base := time.Now()
	skewed := domain.NetworkCall{
		StartedAt: base.Add(9 * time.Second), EndedAt: base.Add(30 * time.Second),
		HasRequest: true, HasResponse: true,
	}
	offset, width := waterfallBar(skewed, base, 10*time.Second)
	if offset+width > 100.01 {
		t.Errorf("bar runs past its track: %.1f + %.1f", offset, width)
	}
}

func TestCallWindowSpansEverything(t *testing.T) {
	base := time.Now()
	calls := []domain.NetworkCall{
		call(base.Add(5*time.Second), time.Second, true),
		call(base, time.Second, true),
		call(base.Add(8*time.Second), 2*time.Second, true),
	}
	start, span := callWindow(calls)
	if !start.Equal(base) {
		t.Errorf("the window should start at the earliest call, got %s", start)
	}
	if span != 10*time.Second {
		t.Errorf("the window should reach the latest end, got %s", span)
	}
}

// Calls in the same millisecond would otherwise divide by zero.
func TestCallWindowNeverCollapses(t *testing.T) {
	base := time.Now()
	_, span := callWindow([]domain.NetworkCall{call(base, 0, true), call(base, 0, true)})
	if span <= 0 {
		t.Fatalf("span must stay positive, got %s", span)
	}
	if _, width := waterfallBar(call(base, 0, true), base, span); width <= 0 {
		t.Errorf("a zero-length window still has to render, got width %.2f", width)
	}
}

func TestDurationLabel(t *testing.T) {
	base := time.Now()
	cases := map[string]domain.NetworkCall{
		"118ms": call(base, 118*time.Millisecond, true),
		"1.4s":  call(base, 1400*time.Millisecond, true),
		"<1ms":  call(base, 200*time.Microsecond, true),
		"—":     {StartedAt: base, EndedAt: base, HasRequest: true},
	}
	for want, c := range cases {
		if got := durationLabel(c); got != want {
			t.Errorf("want %q, got %q", want, got)
		}
	}
}

// Only tabs with something behind them are offered. A Response tab on a call
// that never came back can only ever say "nothing here", and a Payload tab on
// a GET with no body is the same.
func TestAvailableTabsAndDefault(t *testing.T) {
	phase := func(p string, meta string) domain.Log {
		cp := domain.CallPhase(p)
		l := domain.Log{CallPhase: &cp}
		if meta != "" {
			raw := json.RawMessage(meta)
			l.Metadata = &raw
		}
		return l
	}

	// A GET that answered: no body went out, so no Payload tab.
	get := []domain.Log{phase("request", `{"method":"GET"}`), phase("response", `{"status_code":200}`)}
	if got := AvailableTabs(get); !reflect.DeepEqual(got, []string{"headers", "response"}) {
		t.Errorf("want headers then response, got %v", got)
	}
	// The response body is what you usually came to read.
	if got := DefaultTab(get); got != "response" {
		t.Errorf("want response, got %q", got)
	}

	// A POST that answered: the request body earns a Payload tab.
	post := []domain.Log{
		phase("request", `{"method":"POST","body":"{\"a\":1}"}`),
		phase("response", `{"status_code":201}`),
	}
	if got := AvailableTabs(post); !reflect.DeepEqual(got, []string{"headers", "payload", "response"}) {
		t.Errorf("want headers, payload, response, got %v", got)
	}

	// An error is why you clicked, so it wins.
	failed := []domain.Log{phase("request", `{"method":"GET"}`), phase("error", `{"type":"timeout"}`)}
	if got := AvailableTabs(failed); !reflect.DeepEqual(got, []string{"headers", "error"}) {
		t.Errorf("want headers then error, got %v", got)
	}
	if got := DefaultTab(failed); got != "error" {
		t.Errorf("want error, got %q", got)
	}

	// Still in flight: headers are all there is, and they are enough to see
	// what was asked for.
	pending := []domain.Log{phase("request", `{"method":"GET"}`)}
	if got := AvailableTabs(pending); !reflect.DeepEqual(got, []string{"headers"}) {
		t.Errorf("a pending call offers headers only, got %v", got)
	}
	if got := DefaultTab(pending); got != "headers" {
		t.Errorf("want headers, got %q", got)
	}

	if got := DefaultTab(nil); got != "" {
		t.Errorf("nothing recorded means no tab, got %q", got)
	}

	// A tab= naming something this call does not have must not stick, or the
	// pane renders empty with nothing highlighted.
	if TabAvailable(get, "payload") {
		t.Error("a GET with no body should not accept tab=payload")
	}
	if !TabAvailable(get, "headers") {
		t.Error("headers should always be available once anything was recorded")
	}
}

// Headers must not shuffle between two views of the same call.
func TestSortedKVIsStable(t *testing.T) {
	headers := map[string]any{
		"x-request-id": "abc",
		"content-type": "application/json",
		"accept":       "*/*",
		// A header can legitimately be a list; Set-Cookie usually is.
		"set-cookie": []any{"a=1", "b=2"},
	}
	first := sortedKV(headers)
	second := sortedKV(headers)

	if len(first) != 4 {
		t.Fatalf("want 4 headers, got %d", len(first))
	}
	if first[0].Key != "accept" {
		t.Errorf("want alphabetical, got %s first", first[0].Key)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("order changed between renders at %d: %v vs %v", i, first[i], second[i])
		}
	}
	for _, row := range first {
		if row.Key == "set-cookie" && row.Value != "a=1, b=2" {
			t.Errorf("a list header should join, got %q", row.Value)
		}
	}
}

// A body arrives as whatever the app sent: an object, a string, or nothing.
func TestRawFieldRendersAnyBody(t *testing.T) {
	meta := map[string]any{
		"object": map[string]any{"qty": float64(2)},
		"string": "plain text",
		"null":   nil,
	}
	if got := rawField(meta, "object"); got != "{\n  \"qty\": 2\n}" {
		t.Errorf("an object body should pretty-print, got %q", got)
	}
	if got := rawField(meta, "string"); got != "plain text" {
		t.Errorf("a string body should pass through, got %q", got)
	}
	if got := rawField(meta, "null"); got != "" {
		t.Errorf("no body should render nothing, got %q", got)
	}
	if got := rawField(meta, "missing"); got != "" {
		t.Errorf("a missing key should render nothing, got %q", got)
	}
}

// The two hrefs must always agree: the address pushed into the bar has to be
// the one that rebuilds the same view on a refresh.
func TestNetworkHrefsCarryTheFilter(t *testing.T) {
	projectID := uuid.New()
	requestID := uuid.New()
	f := domain.NetworkFilter{Path: "/v2/cart", Method: "GET", Status: "4xx"}

	page := networkHref(projectID, f, requestID, "response")
	fragment := inspectorHref(projectID, f, requestID, "response")

	for _, want := range []string{"path=%2Fv2%2Fcart", "method=GET", "status=4xx", "rid=" + requestID.String(), "tab=response"} {
		if !contains(page, want) {
			t.Errorf("the page href lost %s: %s", want, page)
		}
		if !contains(fragment, want) {
			t.Errorf("the fragment href lost %s: %s", want, fragment)
		}
	}
	if !contains(page, "/network?") || contains(page, "/network/inspector") {
		t.Errorf("the pushed URL must be the page, got %s", page)
	}
	if !contains(fragment, "/network/inspector?") {
		t.Errorf("the fetched URL must be the fragment endpoint, got %s", fragment)
	}
}

// The pane's id is what every row targets. htmx fails silently when the target
// is missing, so a click would just do nothing.
func TestDetailPaneAlwaysCarriesItsTargetID(t *testing.T) {
	empty := render(t, NetworkDetailPane(NetworkData{ProjectID: uuid.New()}))
	if !contains(empty, `id="network-detail"`) {
		t.Errorf("the empty pane lost its id: %s", empty)
	}

	body := json.RawMessage(`{"headers":{"accept":"*/*"},"body":"{}"}`)
	phase := domain.CallPhase("response")
	selected := domain.NetworkCall{RequestID: uuid.New(), HasRequest: true, HasResponse: true}
	filled := render(t, NetworkDetailPane(NetworkData{
		ProjectID: uuid.New(),
		Selected:  &selected,
		Phases:    []domain.Log{{CallPhase: &phase, Metadata: &body}},
		Tab:       "response",
	}))
	if !contains(filled, `id="network-detail"`) {
		t.Errorf("the filled pane lost its id: %s", filled)
	}
}

// The SDK strips credentials before a request ever leaves the device, and the
// dashboard has to show that as a deliberate absence rather than printing the
// marker as though it were the header's value.
func TestRedactedHeaderRendersAsRedaction(t *testing.T) {
	meta := json.RawMessage(`{"headers":{"authorization":"[redacted]","accept":"*/*"}}`)
	phase := domain.CallPhase("request")
	selected := domain.NetworkCall{RequestID: uuid.New(), HasRequest: true}

	out := render(t, NetworkDetailPane(NetworkData{
		ProjectID: uuid.New(),
		Selected:  &selected,
		Phases:    []domain.Log{{CallPhase: &phase, Metadata: &meta}},
		Tab:       "headers",
	}))

	if !contains(out, "data-redacted") {
		t.Errorf("a redacted header should render as a redaction, got %s", out)
	}
	if contains(out, "[redacted]") {
		t.Errorf("the marker leaked into the page as content: %s", out)
	}
	// The header name stays, so you can still see what was sent.
	if !contains(out, "authorization") {
		t.Errorf("the header name should survive, got %s", out)
	}
	// And an ordinary header is untouched.
	if !contains(out, "*/*") {
		t.Errorf("a normal header should render normally, got %s", out)
	}
}
