package view

import (
	"strings"
	"testing"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/google/uuid"
)

func gridFor(page domain.LiveDBPage, writable bool) LiveDBGridData {
	return LiveDBGridData{
		Project:   &domain.Project{},
		SessionID: uuid.New(),
		Source:    "shop.db",
		Table:     "users",
		PageSize:  100,
		Writable:  writable,
		Filters:   map[string]string{},
		Page:      page,
	}
}

func renderGrid(t *testing.T, d LiveDBGridData) string {
	t.Helper()
	return render(t, LiveDBGrid(d))
}

// encoding/json turns every number into a float64, so a rowid of 4 arrives as
// 4.0 and formats as "4e+00" with the default verb. Every handle in the grid
// and every value in a numeric column goes through this.
func TestScalarStringDoesNotRenderNumbersInExponentForm(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{float64(4), "4"},
		{float64(1754301882431), "1754301882431"},
		{float64(12.5), "12.5"},
		{float64(0), "0"},
		{"ada@example.com", "ada@example.com"},
		{true, "true"},
		{nil, "NULL"},
	}
	for _, c := range cases {
		if got := scalarString(c.in); got != c.want {
			t.Errorf("scalarString(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A value goes back to the device as the JSON type it arrived as. The conflict
// check compares with SQLite's IS, and 4 IS '4' is false because the types
// differ — a rowid sent back as a string would never match its own row.
func TestScalarJSONKeepsTheType(t *testing.T) {
	if got := scalarJSON(float64(4)); got != "4" {
		t.Errorf("a numeric handle became %q", got)
	}
	if got := scalarJSON("4"); got != `"4"` {
		t.Errorf("a string handle became %q, losing the quotes that make it a string", got)
	}
	if got := scalarJSON(nil); got != "null" {
		t.Errorf("null became %q", got)
	}
}

func TestUnquoteJSONForTheInput(t *testing.T) {
	cases := []struct{ in, want string }{
		{`"ada"`, "ada"},
		{`4`, "4"},
		{`null`, ""},
		{``, ""},
		{`12.5`, "12.5"},
	}
	for _, c := range cases {
		if got := unquoteJSON(c.in); got != c.want {
			t.Errorf("unquoteJSON(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestGridRendersNullDistinctlyFromEmpty(t *testing.T) {
	// In a database the difference between NULL and "" is real, and a grid that
	// renders both as a blank cell hides the thing somebody opened it to see.
	html := renderGrid(t, gridFor(domain.LiveDBPage{
		Columns: []domain.LiveDBColumn{{Name: "note"}},
		Rows:    [][]domain.LiveDBCell{{{Value: nil}}, {{Value: ""}}},
		Handles: []any{float64(1), float64(2)},
	}, false))

	if !strings.Contains(html, "NULL") {
		t.Error("a null cell did not render as NULL")
	}
}

func TestGridRendersARedactedCellAsARedaction(t *testing.T) {
	html := renderGrid(t, gridFor(domain.LiveDBPage{
		Columns: []domain.LiveDBColumn{{Name: "auth_token", Redacted: true}},
		Rows: [][]domain.LiveDBCell{{{
			Value:    RedactedMarker,
			ReadOnly: "This column is redacted.",
		}}},
		Handles: []any{float64(1)},
	}, true))

	if !strings.Contains(html, RedactedMarker) {
		t.Error("the redaction marker was not rendered")
	}
	// It must not become an editor: there is nothing to compare a redacted value
	// against, so offering the button would produce a write that always fails.
	if strings.Contains(html, "/cell?") {
		t.Error("a redacted cell was offered as editable")
	}
}

func TestGridOnlyOffersAnEditorWhenEverythingAllowsIt(t *testing.T) {
	page := domain.LiveDBPage{
		Columns: []domain.LiveDBColumn{{Name: "enabled", Type: "INTEGER"}},
		Rows:    [][]domain.LiveDBCell{{{Value: float64(1)}}},
		Handles: []any{float64(4)},
	}

	if html := renderGrid(t, gridFor(page, true)); !strings.Contains(html, "/cell?") {
		t.Error("an editable cell in a writable source was not offered as editable")
	}

	// The source was registered for browsing only.
	if html := renderGrid(t, gridFor(page, false)); strings.Contains(html, "/cell?") {
		t.Error("a read-only source offered an editor")
	}

	// The device said this particular cell cannot be edited.
	locked := page
	locked.Rows = [][]domain.LiveDBCell{{{Value: "<blob · 42.1 KB>", ReadOnly: "Binary data."}}}
	if html := renderGrid(t, gridFor(locked, true)); strings.Contains(html, "/cell?") {
		t.Error("a cell the device locked was offered as editable")
	}

	// A view has no handle, so there is no row to address.
	noHandle := page
	noHandle.Handles = []any{nil}
	if html := renderGrid(t, gridFor(noHandle, true)); strings.Contains(html, "/cell?") {
		t.Error("a row with no handle was offered as editable")
	}
}

func TestGridSaysWhenItStoppedEarly(t *testing.T) {
	// A page that quietly stops short reads as "that is all there is", which is
	// the one wrong impression a database browser must not give.
	html := renderGrid(t, gridFor(domain.LiveDBPage{
		Columns:        []domain.LiveDBColumn{{Name: "note"}},
		Rows:           [][]domain.LiveDBCell{{{Value: "x"}}},
		Handles:        []any{float64(1)},
		HasMore:        true,
		StoppedForSize: true,
	}, false))

	if !strings.Contains(html, "too big to send") {
		t.Error("a page truncated for size did not say so")
	}
}

func TestGridShowsTheDeviceErrorRatherThanAnEmptyTable(t *testing.T) {
	d := gridFor(domain.LiveDBPage{}, false)
	d.Error = "The device did not answer."
	html := renderGrid(t, d)

	if !strings.Contains(html, "The device did not answer.") {
		t.Error("the reason was not rendered")
	}
	if strings.Contains(html, "<table") {
		t.Error("an empty table was rendered alongside the error, which reads as no data")
	}
}

func TestCellEditorCarriesWhatTheWriteNeeds(t *testing.T) {
	html := render(t, LiveDBCellEditor(LiveDBCellData{
		Project:   &domain.Project{},
		SessionID: uuid.New(),
		Source:    "shop.db",
		Table:     "flags",
		Column:    "enabled",
		Handle:    "4",
		Was:       "0",
		Type:      "INTEGER",
	}))

	// `was` is the whole conflict check. Without it on the form the device has
	// nothing to compare and every write silently overwrites.
	for _, want := range []string{`name="was"`, `name="handle"`, `name="column"`, `name="table"`, `name="db"`} {
		if !strings.Contains(html, want) {
			t.Errorf("the form is missing %s", want)
		}
	}
	// The statement is shown because that is what makes "the device builds the
	// SQL, not the dashboard" checkable rather than a claim.
	if !strings.Contains(html, "UPDATE") || !strings.Contains(html, "the value you saw") {
		t.Error("the editor did not show the statement that will run")
	}
}

func TestNullableColumnOffersNullAndNotNullDoesNot(t *testing.T) {
	renderCell := func(notNull bool) string {
		return render(t, LiveDBCellEditor(LiveDBCellData{
			Project: &domain.Project{}, SessionID: uuid.New(),
			Column: "note", Handle: "1", Was: `"x"`, NotNull: notNull,
		}))
	}

	if !strings.Contains(renderCell(false), `name="null"`) {
		t.Error("a nullable column did not offer NULL")
	}
	if strings.Contains(renderCell(true), `name="null"`) {
		t.Error("a NOT NULL column offered NULL, which the device would refuse anyway")
	}
}

func TestSaveResultTellsTheThreeOutcomesApart(t *testing.T) {
	renderSave := func(d LiveDBSaveData) string {
		d.Project = &domain.Project{}
		return render(t, LiveDBSaveResult(d))
	}

	ok := renderSave(LiveDBSaveData{OK: true, Table: "flags", Column: "enabled"})
	if !strings.Contains(ok, "holding the old value in memory") {
		t.Error("a successful write did not warn about the app's in-memory copy")
	}

	conflict := renderSave(LiveDBSaveData{Code: "row_changed", Current: float64(5)})
	if !strings.Contains(conflict, "changed this row first") || !strings.Contains(conflict, "5") {
		t.Error("a conflict did not say what the row holds now")
	}

	refused := renderSave(LiveDBSaveData{Message: "This column cannot be empty."})
	if !strings.Contains(refused, "cannot be empty") {
		t.Error("a refusal did not carry its reason")
	}
}

func TestReplyReasonPrefersWhicheverFieldTheDeviceUsed(t *testing.T) {
	// The SDK says "error" for a command it could not run and "message" for a
	// write it refused. A screen has one place to put either.
	if got := (domain.LiveDBReply{Error: "no such table"}).Reason(); got != "no such table" {
		t.Errorf("got %q", got)
	}
	if got := (domain.LiveDBReply{Message: "browsing only"}).Reason(); got != "browsing only" {
		t.Errorf("got %q", got)
	}
	if got := (domain.LiveDBReply{}).Reason(); got == "" {
		t.Error("a reply with neither field said nothing at all")
	}
}

// The device stops building a page early when it would outgrow the frame, so a
// truncated page holds fewer rows than it asked for. Advancing by the page size
// then skipped every row it had dropped, and Prev came back to where you
// already were — the rows in between could not be reached from the screen.
func TestNextAdvancesByRowsReturnedNotPageSize(t *testing.T) {
	page := domain.LiveDBPage{
		Columns:        []domain.LiveDBColumn{{Name: "note"}},
		Handles:        []any{},
		HasMore:        true,
		StoppedForSize: true,
	}
	// 25 rows returned out of a requested 100.
	for i := 0; i < 25; i++ {
		page.Rows = append(page.Rows, []domain.LiveDBCell{{Value: "x"}})
		page.Handles = append(page.Handles, float64(i+1))
	}

	d := gridFor(page, false)
	d.Offset = 0
	html := renderGrid(t, d)

	if !strings.Contains(html, "offset=25") {
		t.Error("Next did not advance by the rows actually returned")
	}
	if strings.Contains(html, "offset=100") {
		t.Error("Next jumped a full page, skipping the rows the device dropped")
	}
}

// The panel exists to make "the device builds the statement, not the dashboard"
// checkable rather than a promise, so it has to describe what the device will
// actually do. It printed "rowid" unconditionally, which is wrong for every
// key-value store: those address rows by key and run no SQL at all.
func TestTheStatementPreviewMatchesWhatTheDeviceWillDo(t *testing.T) {
	sql := render(t, LiveDBCellEditor(LiveDBCellData{
		Project: &domain.Project{}, SessionID: uuid.New(),
		Table: "flags", Column: "enabled", Handle: "4", Was: "0",
		HandleColumn: "rowid", Kind: "sql",
	}))
	if !strings.Contains(sql, "UPDATE") || !strings.Contains(sql, "rowid IS ?") {
		t.Error("a SQL source did not show the UPDATE it will run")
	}

	kv := render(t, LiveDBCellEditor(LiveDBCellData{
		Project: &domain.Project{}, SessionID: uuid.New(),
		Table: "prefs", Column: "value", Handle: `"locale"`, Was: `"en_GB"`,
		HandleColumn: "key", Kind: "keyValue",
	}))
	if strings.Contains(kv, "rowid") {
		t.Error("a key-value store was shown a statement addressing rowid")
	}
	if strings.Contains(kv, "UPDATE") {
		t.Error("a key-value store was shown SQL, which its write never runs")
	}
	if !strings.Contains(kv, "locale") || !strings.Contains(kv, "en_GB") {
		t.Error("the key-value preview did not name the key or the value it guards on")
	}
}

// A SQL table whose handle is not rowid still has to be described correctly,
// which is why the name travels on the page rather than being assumed.
func TestThePreviewUsesWhateverHandleTheDeviceReported(t *testing.T) {
	html := render(t, LiveDBCellEditor(LiveDBCellData{
		Project: &domain.Project{}, SessionID: uuid.New(),
		Table: "t", Column: "c", Handle: "1", Was: "0",
		HandleColumn: "oid", Kind: "sql",
	}))
	if !strings.Contains(html, "oid IS ?") {
		t.Error("the preview ignored the handle column the device named")
	}
}
