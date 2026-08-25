package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/live"
	"github.com/getcodescout/code_scout/server/middleware"
	"github.com/getcodescout/code_scout/view"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// The database browser's end of a live session.
//
// Every one of these is a round trip through a phone: dashboard, server,
// handset on whatever network it is on, and back. That shapes the whole file.
// Nothing here is cached, nothing is stored, and every handler answers 200 with
// the outcome in the markup — htmx does not swap the body of a non-2xx
// response, so a 502 for "the device is asleep" would leave the page sitting
// there with no explanation.

// askDevice puts one question to the device behind the session in the URL and
// decodes its answer.
//
// Returns a reply with OK false rather than an error for anything the person
// looking at the screen needs to read.
func (h *LiveHandler) askDevice(
	w http.ResponseWriter, r *http.Request, op string, args map[string]any,
) (*domain.Project, uuid.UUID, domain.LiveDBReply, bool) {
	var reply domain.LiveDBReply

	project := h.projectFromRequest(w, r)
	if project == nil {
		return nil, uuid.Nil, reply, false
	}

	sessionID, err := uuid.Parse(mux.Vars(r)["sid"])
	if err != nil {
		http.Error(w, "Invalid session", http.StatusBadRequest)
		return nil, uuid.Nil, reply, false
	}
	// Scoped to the project in the URL, which the access middleware has already
	// checked. Without this, knowing a session id would be enough to read a
	// phone belonging to a project you cannot see.
	if s, ok := h.hub.Get(sessionID); !ok || s.ProjectID != project.ID {
		http.Error(w, "Not found", http.StatusNotFound)
		return nil, uuid.Nil, reply, false
	}

	raw, err := h.hub.Ask(r.Context(), sessionID, live.DeviceCommand{Op: op, Args: args})
	if err != nil {
		reply.Error = deviceAskMessage(err)
		return project, sessionID, reply, true
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		reply.Error = "The device sent something this dashboard could not read."
		return project, sessionID, reply, true
	}
	return project, sessionID, reply, true
}

// deviceAskMessage turns a hub error into something worth reading. The two
// cases are genuinely different to the person looking at the screen: one means
// the session is over, the other means their rows are late.
func deviceAskMessage(err error) string {
	switch {
	case errors.Is(err, live.ErrDeviceSilent):
		return "The device did not answer. It is probably in the background — open the app and try again."
	case errors.Is(err, live.ErrDeviceGone), errors.Is(err, live.ErrNoSuchSession):
		return "The device is no longer connected, so its databases cannot be read."
	default:
		return "Could not reach the device."
	}
}

// LiveDatabase renders the Database pane: what this app has offered up, and the
// tables inside whichever one is selected.
func (h *LiveHandler) LiveDatabase(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	project, sessionID, reply, ok := h.askDevice(w, r, "sources", nil)
	if !ok {
		return
	}

	data := view.LiveDBData{
		Project:   project,
		SessionID: sessionID,
		Sources:   reply.Sources,
		Error:     firstError(reply),
	}

	// Which database to open. The first registered by default, because an app
	// nearly always registers its main one first.
	selected := r.URL.Query().Get("db")
	if selected == "" && len(reply.Sources) > 0 {
		selected = reply.Sources[0].Name
	}
	data.Selected = selected

	if selected != "" && data.Error == "" {
		_, _, ns, ok := h.askDevice(w, r, "namespaces", map[string]any{"db": selected})
		if !ok {
			return
		}
		if ns.OK {
			data.Namespaces = ns.Namespaces
		} else {
			data.Error = ns.Reason()
		}
		for _, s := range reply.Sources {
			if s.Name == selected {
				data.Writable = s.Writable
			}
		}
	}

	view.LiveDBPane(data).Render(ctx, w)
}

// LiveDatabaseRows renders one page of one table.
func (h *LiveHandler) LiveDatabaseRows(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	args := map[string]any{
		"db":        q.Get("db"),
		"namespace": q.Get("table"),
		"limit":     pageSize,
		"offset":    atoiDefault(q.Get("offset"), 0),
	}
	if sort := q.Get("sort"); sort != "" {
		args["sort"] = sort
		args["desc"] = q.Get("dir") == "desc"
	}
	// One input per column rather than an expression language: no grammar to
	// document and no parser on the device.
	filters := map[string]any{}
	for key, values := range q {
		if len(key) > 2 && key[:2] == "f_" && len(values) > 0 && values[0] != "" {
			filters[key[2:]] = values[0]
		}
	}
	if len(filters) > 0 {
		args["filters"] = filters
	}

	project, sessionID, reply, ok := h.askDevice(w, r, "rows", args)
	if !ok {
		return
	}

	data := view.LiveDBGridData{
		Project:   project,
		SessionID: sessionID,
		Source:    q.Get("db"),
		Table:     q.Get("table"),
		Sort:      q.Get("sort"),
		Desc:      q.Get("dir") == "desc",
		Offset:    atoiDefault(q.Get("offset"), 0),
		PageSize:  pageSize,
		// From the device's reply, never from anything the browser sent about
		// itself. The registry on the device enforces writes regardless, so a
		// spoofed flag could only ever draw buttons that get refused — but a
		// dashboard should not draw affordances it cannot honour.
		Writable: reply.Writable,
		Filters:  map[string]string{},
		Error:    firstError(reply),
	}
	for k := range filters {
		data.Filters[k] = q.Get("f_" + k)
	}
	if reply.Page != nil {
		data.Page = *reply.Page
	}

	view.LiveDBGrid(data).Render(ctx, w)
}

// LiveDatabaseCell opens the editor for one cell.
//
// No round trip to the device: everything it needs was already on the page the
// grid rendered. Asking the phone again would also be wrong, not just slow —
// the value it answered with would be the current one, and the whole point of
// carrying `was` is that it is the value this screen was actually shown.
func (h *LiveHandler) LiveDatabaseCell(w http.ResponseWriter, r *http.Request) {
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
	// The same scoping the round-trip handlers get from askDevice. This one
	// never talks to the device, but a session id from another project must
	// not render anything here either.
	if s, ok := h.hub.Get(sessionID); !ok || s.ProjectID != project.ID {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	q := r.URL.Query()

	// Where Esc puts the grid back. It is a URL out of the query string, so it
	// is pinned to this session's own db routes rather than trusted: hx-get
	// will fetch whatever it is given, and same-origin is not the same thing
	// as intended.
	back := q.Get("back")
	if !strings.HasPrefix(back, fmt.Sprintf("/project/%s/live/%s/db", project.ID, sessionID)) {
		back = fmt.Sprintf("/project/%s/live/%s/db/rows?db=%s&table=%s",
			project.ID, sessionID, url.QueryEscape(q.Get("db")), url.QueryEscape(q.Get("table")))
	}

	view.LiveDBCellEditor(view.LiveDBCellData{
		Project:      project,
		SessionID:    sessionID,
		Source:       q.Get("db"),
		Table:        q.Get("table"),
		Column:       q.Get("column"),
		Handle:       q.Get("handle"),
		Was:          q.Get("was"),
		Type:         q.Get("type"),
		NotNull:      q.Get("notnull") == "true",
		HandleColumn: q.Get("handle_col"),
		Kind:         q.Get("kind"),
		Back:         back,
	}).Render(ctx, w)
}

// LiveDatabaseSave changes one cell.
//
// Behind RequireProjectManage, unlike everything else in a live session. The
// rest of it shows you what the log viewer would have shown you anyway; this
// reaches into somebody's device and changes what is stored there, which is
// closer to rotating a project secret than to reading a stream.
func (h *LiveHandler) LiveDatabaseSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	actor := middleware.UserFrom(ctx)
	args := map[string]any{
		"db":        r.FormValue("db"),
		"namespace": r.FormValue("table"),
		"column":    r.FormValue("column"),
		"handle":    jsonScalar(r.FormValue("handle")),
		"was":       jsonScalar(r.FormValue("was")),
		// Who did it, for the audit line the device writes into its own log
		// stream on a successful edit. The device is the right place for that
		// record: it syncs with everything else and lands in the session
		// timeline, where "why is this device's data odd" gets asked.
		"by": actorEmail(actor),
	}
	// A cleared field and "set this to NULL" are different intentions, so the
	// form says which rather than making the device guess from an empty string.
	if r.FormValue("null") != "1" {
		args["value"] = r.FormValue("value")
	}

	project, sessionID, reply, ok := h.askDevice(w, r, "update", args)
	if !ok {
		return
	}

	// Tells the grid to reload itself. Without it the edited cell keeps showing
	// its open editor and the value it had before, which reads as a save that
	// did nothing.
	if reply.OK {
		w.Header().Set("HX-Trigger", "cs-db-saved")
	}

	view.LiveDBSaveResult(view.LiveDBSaveData{
		Project:   project,
		SessionID: sessionID,
		Source:    r.FormValue("db"),
		Table:     r.FormValue("table"),
		Column:    r.FormValue("column"),
		OK:        reply.OK,
		Code:      reply.Code,
		Message:   firstError(reply),
		Current:   reply.Current,
	}).Render(ctx, w)
}

// pageSize is what the dashboard asks for. The device enforces its own ceiling
// regardless, because a limit the caller can raise is not a limit.
const pageSize = 100

func firstError(reply domain.LiveDBReply) string {
	if reply.OK {
		return ""
	}
	return reply.Reason()
}

func atoiDefault(s string, fallback int) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

// jsonScalar turns a form field back into the JSON value it came from.
//
// Every field on the editor's form is text, and the device compares the handle
// and the old value with SQLite's IS. Where the column has an affinity SQLite
// converts first, so a rowid sent as "4" would still match: that is why this is
// easy to get wrong and see nothing. A column declared with no type has no
// affinity and converts nothing, so `v IS '25'` against the integer 25 is
// false, and an edit to a row nobody has touched is refused as a conflict.
//
// `test/e2e/db_browser_test.dart` in the SDK repo edits exactly such a column.
func jsonScalar(s string) any {
	if s == "" {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err == nil {
		return v
	}
	// Not valid JSON, so it was always a plain string.
	return s
}

func actorEmail(u *domain.User) string {
	if u == nil {
		return ""
	}
	return u.Email
}
