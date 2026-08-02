package domain

import (
	"sort"
	"strings"
)

// Query serialises a filter back into the search syntax it was parsed from.
//
// This is what makes the toolbar work without any client-side state: each
// toggle is a link to the query you would have after clicking it, computed here
// and put straight in the URL. One consequence is that a pasted link reproduces
// the view exactly, because the URL is the only description of it.
//
// Round-tripping matters: Parse(f.Query()) must equal f, or a second click on
// any control would compute from a filter that had quietly lost something.
func (f SearchFilter) Query() string {
	var parts []string

	// Sorted so the same set of clicks always produces the same string. Without
	// it the URL would churn on every toggle and browser history would fill
	// with entries that mean the same thing.
	for _, level := range sortedCopy(f.Levels) {
		parts = append(parts, "level:"+level)
	}
	for _, tag := range sortedCopy(f.Tags) {
		parts = append(parts, "tag:"+tag)
	}
	for _, tag := range sortedCopy(f.ExcludeTags) {
		parts = append(parts, "-tag:"+tag)
	}
	if f.IsNetwork != nil && *f.IsNetwork {
		parts = append(parts, "is:network")
	}
	if f.SinceLabel != "" {
		parts = append(parts, "last:"+f.SinceLabel)
	}
	if f.SessionID != nil {
		parts = append(parts, "session:"+f.SessionID.String())
	}
	if f.RequestID != nil {
		parts = append(parts, "request:"+f.RequestID.String())
	}
	if f.TextQuery != "" {
		// Quoted whenever it could not survive a round trip bare: spaces would
		// split into separate words, and a colon would read as a field.
		if strings.ContainsAny(f.TextQuery, " \t:\"") {
			parts = append(parts, `"`+strings.ReplaceAll(f.TextQuery, `"`, "")+`"`)
		} else {
			parts = append(parts, f.TextQuery)
		}
	}

	return strings.Join(parts, " ")
}

// IsEmpty reports whether anything is being filtered at all, which is what the
// "clear filters" affordance keys off.
func (f SearchFilter) IsEmpty() bool { return f.Query() == "" }

// WithLevelToggled switches one level on or off.
//
// Turning the last one off means "show everything" rather than "show nothing":
// an empty list is how an unfiltered view is expressed, and a viewer holding
// zero levels would be a screen that can only be empty.
func (f SearchFilter) WithLevelToggled(level string) SearchFilter {
	out := f.clone()
	if !f.HasLevel(level) {
		out.Levels = append(out.Levels, level)
		return out
	}
	// Currently shown. If nothing is filtered, clicking one level means "only
	// this one" rather than "all except this one" — clicking a single toggle
	// should narrow, which is what people expect of a filter.
	if len(f.Levels) == 0 {
		out.Levels = []string{level}
		return out
	}
	out.Levels = without(f.Levels, level)
	return out
}

// WithTagCycled moves a tag through neutral, included, excluded and back.
func (f SearchFilter) WithTagCycled(tag string) SearchFilter {
	out := f.clone()
	switch f.StateForTag(tag) {
	case TagNeutral:
		out.Tags = append(out.Tags, tag)
	case TagIncluded:
		out.Tags = without(out.Tags, tag)
		out.ExcludeTags = append(out.ExcludeTags, tag)
	case TagExcluded:
		out.ExcludeTags = without(out.ExcludeTags, tag)
	}
	return out
}

// WithWindow sets the time window, or clears it when given the label already
// active so the same control turns it off.
func (f SearchFilter) WithWindow(label string) SearchFilter {
	out := f.clone()
	if f.SinceLabel == label {
		out.SinceLabel = ""
		out.Since = nil
		return out
	}
	out.SinceLabel = label
	return out
}

// WithNetworkOnly toggles the network-only filter.
func (f SearchFilter) WithNetworkOnly() SearchFilter {
	out := f.clone()
	if f.IsNetwork != nil && *f.IsNetwork {
		out.IsNetwork = nil
		return out
	}
	on := true
	out.IsNetwork = &on
	return out
}

// clone copies the slices as well as the struct, so a returned filter cannot
// share backing arrays with the one it came from — append would otherwise
// sometimes mutate the original.
func (f SearchFilter) clone() SearchFilter {
	out := f
	out.Levels = append([]string(nil), f.Levels...)
	out.Tags = append([]string(nil), f.Tags...)
	out.ExcludeTags = append([]string(nil), f.ExcludeTags...)
	return out
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func without(in []string, drop string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != drop {
			out = append(out, s)
		}
	}
	return out
}
