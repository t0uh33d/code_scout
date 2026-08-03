package view

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/getcodescout/code_scout/internal/domain"
)

// networkHref is the page address a selection corresponds to — what goes in the
// address bar. inspectorHref is the fragment endpoint that actually answers the
// click. They are kept side by side because they must always agree: the URL
// pushed and the URL that would rebuild the same view.
func networkHref(projectID uuid.UUID, f domain.NetworkFilter, requestID uuid.UUID, tab string) string {
	return fmt.Sprintf("/project/%s/network?%s", projectID, networkQuery(f, requestID, tab))
}

func inspectorHref(projectID uuid.UUID, f domain.NetworkFilter, requestID uuid.UUID, tab string) string {
	return fmt.Sprintf("/project/%s/network/inspector?%s", projectID, networkQuery(f, requestID, tab))
}

// The filter travels with the selection so a swapped-in detail pane's own links
// still know what the list is filtered to. Without it, clicking a tab would
// push a URL that reloads to an unfiltered list.
func networkQuery(f domain.NetworkFilter, requestID uuid.UUID, tab string) string {
	q := url.Values{}
	if f.Path != "" {
		q.Set("path", f.Path)
	}
	if f.Method != "" {
		q.Set("method", f.Method)
	}
	if f.Status != "" {
		q.Set("status", f.Status)
	}
	if f.SessionID != nil {
		q.Set("session", f.SessionID.String())
	}
	if requestID != uuid.Nil {
		q.Set("rid", requestID.String())
	}
	if tab != "" {
		q.Set("tab", tab)
	}
	return q.Encode()
}

// callWindow is the time span the waterfall is drawn against: the earliest
// start to the latest end across everything on screen. A minimum span keeps a
// burst of calls in the same millisecond from dividing by zero.
func callWindow(calls []domain.NetworkCall) (time.Time, time.Duration) {
	if len(calls) == 0 {
		return time.Time{}, time.Second
	}
	start, end := calls[0].StartedAt, calls[0].EndedAt
	for _, c := range calls {
		if c.StartedAt.Before(start) {
			start = c.StartedAt
		}
		if c.EndedAt.After(end) {
			end = c.EndedAt
		}
	}
	span := end.Sub(start)
	if span < time.Millisecond {
		span = time.Millisecond
	}
	return start, span
}

// waterfallBar is where one call's bar sits, as percentages of the window.
//
// A pending call is drawn from its start to the end of the window, because
// that is what "still going" looks like — a one-pixel stub would read as a call
// that finished instantly.
func waterfallBar(call domain.NetworkCall, start time.Time, span time.Duration) (offset float64, width float64) {
	if span <= 0 {
		return 0, 100
	}
	offset = float64(call.StartedAt.Sub(start)) / float64(span) * 100
	if offset < 0 {
		offset = 0
	}
	if offset > 100 {
		offset = 100
	}

	if call.State() == domain.CallPending {
		width = 100 - offset
	} else {
		width = float64(call.Duration()) / float64(span) * 100
	}
	// Always visible: a fast call in a long window rounds to nothing.
	if width < 2 {
		width = 2
	}
	if offset+width > 100 {
		width = 100 - offset
	}
	return offset, width
}

// durationLabel keeps three significant figures where they matter: sub-second
// calls in milliseconds, anything longer in seconds.
func durationLabel(call domain.NetworkCall) string {
	if call.State() == domain.CallPending {
		return "—"
	}
	d := call.Duration()
	switch {
	case d < time.Millisecond:
		return "<1ms"
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
}

func methodLabel(method *string) string {
	if method == nil || *method == "" {
		return "—"
	}
	return *method
}

// Methods are coloured by how much they can break: reads are quiet, writes are
// warmer, a delete is the one you want to notice in a list.
func methodClass(method *string) string {
	if method == nil {
		return "text-cs-muted"
	}
	switch *method {
	case "GET", "HEAD", "OPTIONS":
		return "text-[#3FB950]"
	case "POST":
		return "text-[#D29922]"
	case "PUT", "PATCH":
		return "text-[#0084FF]"
	case "DELETE":
		return "text-[#F85149]"
	default:
		return "text-cs-muted"
	}
}

func callsSummary(calls []domain.NetworkCall) string {
	if len(calls) == 0 {
		return ""
	}
	var failed int
	for _, c := range calls {
		if c.Failed() {
			failed++
		}
	}
	label := fmt.Sprintf("%d calls", len(calls))
	if len(calls) == 1 {
		label = "1 call"
	}
	if failed == 0 {
		return label
	}
	return fmt.Sprintf("%s · %d failed", label, failed)
}

// availablePhases is the tabs to offer, in the order a call happens. Only the
// phases actually recorded appear.
func availablePhases(phases []domain.Log) []string {
	present := map[string]bool{}
	for _, l := range phases {
		if l.CallPhase != nil {
			present[string(*l.CallPhase)] = true
		}
	}
	var out []string
	for _, phase := range []string{"request", "response", "error"} {
		if present[phase] {
			out = append(out, phase)
		}
	}
	return out
}

// DefaultPhase is the tab to open on. An error is why you clicked, so it wins;
// otherwise the response, which is what you usually want to read.
func DefaultPhase(phases []domain.Log) string {
	available := availablePhases(phases)
	for _, want := range []string{"error", "response", "request"} {
		for _, phase := range available {
			if phase == want {
				return phase
			}
		}
	}
	return ""
}

func phaseLog(phases []domain.Log, tab string) *domain.Log {
	for i := range phases {
		if phases[i].CallPhase != nil && string(*phases[i].CallPhase) == tab {
			return &phases[i]
		}
	}
	return nil
}

func decodeMeta(raw *json.RawMessage) map[string]any {
	if raw == nil || len(*raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(*raw, &m); err != nil {
		return nil
	}
	return m
}

func stringField(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	if s, ok := meta[key].(string); ok {
		return s
	}
	return ""
}

func mapField(meta map[string]any, key string) map[string]any {
	if meta == nil {
		return nil
	}
	if m, ok := meta[key].(map[string]any); ok {
		return m
	}
	return nil
}

// rawField renders a body whatever shape it arrived in. The SDK sends whatever
// the app sent: a JSON object, a string, or nothing.
func rawField(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	value, ok := meta[key]
	if !ok || value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	pretty, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(pretty)
}

// sortedKV puts headers in a stable order, so the same call inspected twice
// does not shuffle its rows around.
func sortedKV(m map[string]any) []kv {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]kv, 0, len(keys))
	for _, k := range keys {
		out = append(out, kv{Key: k, Value: headerValue(m[k])})
	}
	return out
}

// A header can legitimately be a list — Set-Cookie usually is.
func headerValue(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			parts = append(parts, fmt.Sprintf("%v", item))
		}
		return joinComma(parts)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

// RedactedMarker is what the SDK puts in place of a value it stripped. The
// exact string is a contract with the Flutter package's Redactor: change it
// there and the dashboard stops recognising a redaction and prints the marker
// as though it were the header's value.
const RedactedMarker = "[redacted]"

func isRedacted(value string) bool { return value == RedactedMarker }

func headerTitle(tab string) string {
	if tab == "response" {
		return "Response headers"
	}
	return "Request headers"
}

func bodyTitle(tab string) string {
	switch tab {
	case "response":
		return "Response body"
	case "error":
		return "Request body"
	default:
		return "Payload"
	}
}
