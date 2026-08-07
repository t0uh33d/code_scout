package view

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/getcodescout/code_scout/internal/domain"
)

// scalarString renders one value out of a device's database.
//
// The device already turned everything unprintable into something readable — a
// blob into its size, an over-long string into a summary — so this only has to
// deal with what JSON decoding leaves behind. The float case is the one that
// matters: encoding/json makes every number a float64, so a rowid of 4 renders
// as "4e+00" without this and no row can be matched to its handle by eye.
func scalarString(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprint(t)
	}
}

// scalarJSON is how a value goes back to the device.
//
// It has to survive the round trip as the same JSON type it arrived as: the
// conflict check compares with SQLite's IS, and 4 IS '4' is false because the
// types differ. A rowid that came back as a string would never match its row.
func scalarJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func isRedactedCell(c domain.LiveDBCell) bool {
	s, ok := c.Value.(string)
	return ok && s == RedactedMarker
}

// columnMeta is the small grey line under a column name: its declared type, and
// whether it can be null. SQLite columns can have no declared type at all.
func columnMeta(c domain.LiveDBColumn) string {
	parts := []string{}
	if c.Type != "" {
		parts = append(parts, strings.ToLower(c.Type))
	}
	if !c.NotNull {
		parts = append(parts, "null")
	}
	if c.Redacted {
		parts = append(parts, "redacted")
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, " · ")
}

// rowRange is deliberately not "of N". Counting the rows in a table is a scan
// that grows with the table, on a phone, for a number nobody needs.
func rowRange(d LiveDBGridData) string {
	if len(d.Page.Rows) == 0 {
		return "no rows"
	}
	first := d.Offset + 1
	last := d.Offset + len(d.Page.Rows)
	if d.Page.HasMore {
		return fmt.Sprintf("rows %d–%d of more", first, last)
	}
	return fmt.Sprintf("rows %d–%d", first, last)
}

// cellURL opens the editor for one cell, carrying everything the write will
// need: which row, which column, and what this screen was shown. That last one
// is what lets the device reject a row the app changed underneath.
func cellURL(d LiveDBGridData, rowIndex, colIndex int) string {
	if rowIndex >= len(d.Page.Handles) || colIndex >= len(d.Page.Columns) {
		return ""
	}
	column := d.Page.Columns[colIndex]
	cell := d.Page.Rows[rowIndex][colIndex]

	q := url.Values{}
	q.Set("db", d.Source)
	q.Set("table", d.Table)
	q.Set("column", column.Name)
	q.Set("handle", scalarJSON(d.Page.Handles[rowIndex]))
	q.Set("was", scalarJSON(cell.Value))
	q.Set("type", column.Type)
	q.Set("notnull", strconv.FormatBool(column.NotNull))
	q.Set("handle_col", d.Page.RowHandle)
	q.Set("kind", d.Page.Kind)
	q.Set("back", rowsURL(d, d.Offset, d.Sort, d.Desc))

	return fmt.Sprintf("%s/cell?%s", dbBase(d.Project.ID, d.SessionID), q.Encode())
}

// unquoteJSON turns the stored JSON form of a value back into what a person
// should see in the input: "ada" rather than "\"ada\"", and 4 rather than 4.
// NULL becomes an empty box, which is why the NULL button exists separately.
func unquoteJSON(raw string) string {
	if raw == "" || raw == "null" {
		return ""
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}
	if v == nil {
		return ""
	}
	return scalarString(v)
}

// updatePreview is what the device will actually do, shown to the person about
// to do it.
//
// The handle column and the source kind both come from the page the device
// sent, never from a guess here. It used to print "rowid" unconditionally,
// which was wrong for every key-value store: those address rows by key and run
// no SQL at all, so the panel claiming to make "the device builds the
// statement" checkable was itself the thing that was not true.
func updatePreview(d LiveDBCellData) string {
	if d.Kind == "keyValue" {
		if d.Was == "null" || d.Was == "" {
			return fmt.Sprintf("%s[%s] = <the value you type>", d.Table, unquoteJSONOrNull(d.Handle))
		}
		return fmt.Sprintf(
			"%s[%s] = <the value you type>\n\nonly if it still holds %s, the value you saw",
			d.Table, unquoteJSONOrNull(d.Handle), unquoteJSONOrNull(d.Was),
		)
	}

	handle := d.HandleColumn
	if handle == "" {
		handle = "rowid"
	}
	return fmt.Sprintf(
		"UPDATE %s\n   SET %s = ?\n WHERE %s IS ?      -- %s\n   AND %s IS ?      -- %s, the value you saw",
		d.Table, d.Column, handle, d.Handle, d.Column, unquoteJSONOrNull(d.Was),
	)
}

func unquoteJSONOrNull(raw string) string {
	if s := unquoteJSON(raw); s != "" {
		return s
	}
	return "NULL"
}

func dbBannerClass(kind string) string {
	switch kind {
	case "ok":
		return "bg-cs-success/10 text-cs-success"
	case "warn":
		return "bg-cs-warning/10 text-cs-warning"
	default:
		return "bg-cs-error/10 text-cs-error"
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
