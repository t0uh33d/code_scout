package domain

// What a device says when the dashboard asks about its local databases.
//
// These are wire types, like LiveLog and unlike Log: they exist to be decoded
// from a phone's answer and handed to a template, and they carry JSON tags for
// exactly that reason. Nothing here is ever persisted — a device's database
// lives on the device, and a page of its rows is gone the moment the screen
// showing it is closed.

// LiveDBSource is one database an app has offered up.
type LiveDBSource struct {
	Name string `json:"name"`

	// "sql" or "keyValue". Key-value stores are rendered in the same grid, as a
	// two-column table whose row handle is the key.
	Kind string `json:"kind"`

	// Whether the app registered it for editing as well as browsing. False is
	// the default on the device, and the dashboard renders accordingly.
	Writable bool `json:"writable"`
}

// LiveDBNamespace is a table, a view, or a box.
type LiveDBNamespace struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

func (n LiveDBNamespace) IsView() bool { return n.Kind == "view" }

// LiveDBColumn is one column as the device described it.
type LiveDBColumn struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	NotNull    bool   `json:"not_null"`
	PrimaryKey bool   `json:"primary_key"`

	// True when the app's redaction config names this column. The value was
	// replaced on the device, so this only decides how the header renders.
	Redacted bool `json:"redacted"`
}

// LiveDBSchema is the shape of one namespace.
type LiveDBSchema struct {
	Namespace string         `json:"namespace"`
	Columns   []LiveDBColumn `json:"columns"`

	// What identifies a row, or empty when nothing does.
	RowHandle string `json:"row_handle"`

	// Why rows here cannot be changed, in words meant for a person. Set exactly
	// when RowHandle is empty.
	ReadOnlyBecause string `json:"read_only_because"`
}

func (s LiveDBSchema) Editable() bool { return s.RowHandle != "" }

// LiveDBCell is one value, and whether the dashboard is seeing enough of it to
// be allowed to change it.
type LiveDBCell struct {
	Value any `json:"v"`

	// Why this cell is read only. Empty when it is editable.
	//
	// Set by the device for a blob, an over-long string, and a redacted column.
	// The rule is that the dashboard may only edit what it actually saw: an
	// update carries the old value to catch a conflict, and a value nobody was
	// shown cannot be compared against anything.
	ReadOnly string `json:"ro"`
}

func (c LiveDBCell) Editable() bool { return c.ReadOnly == "" }

// IsNull is what the grid renders as a NULL rather than as an empty cell. The
// difference matters in a database and nowhere is it more confusing to lose.
func (c LiveDBCell) IsNull() bool { return c.Value == nil }

// LiveDBPage is one page of rows.
type LiveDBPage struct {
	Columns []LiveDBColumn `json:"columns"`

	// Row-major, parallel to Columns.
	Rows [][]LiveDBCell `json:"rows"`

	// What identifies each row, parallel to Rows. Null throughout when the
	// namespace has no row handle.
	Handles []any `json:"handles"`

	HasMore bool `json:"has_more"`

	// True when the page ended because it was getting too big to send rather
	// than because the rows ran out. Rendered, because a page that quietly
	// stops short reads as "that is all there is".
	StoppedForSize bool `json:"stopped_for_size"`

	// RowHandle names the column that identifies a row — "rowid" for an
	// ordinary SQLite table, "key" for a key-value store. The cell editor shows
	// the statement a write will run, and naming the wrong column there is
	// worse than showing nothing.
	RowHandle string `json:"row_handle"`

	// Kind is "sql" or "keyValue". A key-value write runs no SQL at all, so the
	// editor describes it rather than printing an UPDATE that will never exist.
	Kind string `json:"kind"`
}

// LiveDBReply is the envelope every answer arrives in.
type LiveDBReply struct {
	OK bool `json:"ok"`

	// Why not, when OK is false. Written for a person: it reaches the screen.
	Error   string `json:"error"`
	Message string `json:"message"`

	// Set on a refused write so the screen can say what the row holds now.
	Code    string `json:"code"`
	Current any    `json:"current"`

	Sources    []LiveDBSource    `json:"sources"`
	Namespaces []LiveDBNamespace `json:"namespaces"`
	Schema     *LiveDBSchema     `json:"schema"`
	Writable   bool              `json:"writable"`
	Page       *LiveDBPage       `json:"page"`
}

// Reason is whatever the device gave, whichever field it used. The SDK says
// "error" for a command it could not run and "message" for a write it refused,
// and a screen has one place to put either.
func (r LiveDBReply) Reason() string {
	if r.Error != "" {
		return r.Error
	}
	if r.Message != "" {
		return r.Message
	}
	return "The device could not answer that."
}
