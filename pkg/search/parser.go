package search

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/getcodescout/code_scout/internal/domain"
)

// ParseError represents a search query syntax error with position information.
type ParseError struct {
	Position int
	Message  string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("search parse error at position %d: %s", e.Position, e.Message)
}

var validLevels = map[string]bool{
	"info": true, "debug": true, "warning": true, "error": true,
	"fatal": true, "verbose": true, "system": true,
}

var validFields = map[string]bool{
	"level": true, "tag": true, "is": true, "session": true, "request": true, "last": true,
	"fingerprint": true,
	// Session-scoped. These describe the launch, not the log — see
	// domain.SessionScope.
	"user": true, "device": true, "os": true, "app_version": true, "sdk_version": true,
	"installation": true,
}

// validFieldList is what an error message offers, in a fixed order so the
// message is stable.
//
// It has to be kept in step with validFields above and with the switch that
// consumes them. A field added to one and not the others either parses into
// nothing or is refused despite being handled, and TestEveryValidFieldParses
// exists because both of those have already happened.
const validFieldList = "level, tag, is, last, session, request, fingerprint, user, device, os, app_version, sdk_version, installation"

// windows are the date presets the toolbar offers. A named window rather than a
// timestamp keeps a shared URL meaningful: "last:24h" still means the last 24
// hours tomorrow, where a pasted absolute time would quietly go stale.
var windows = map[string]time.Duration{
	"1h":  time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

// WindowLabels is the offered order, since map iteration has none.
var WindowLabels = []string{"1h", "24h", "7d", "30d"}

// Parse converts a search query string into a SearchFilter.
//
// Supports: level:error (repeatable, OR), tag:auth (repeatable, AND),
// -tag:noise (repeatable, NOT), is:network, last:24h, session:UUID,
// request:UUID, "quoted text" and bare words.
func Parse(query string) (*domain.SearchFilter, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return &domain.SearchFilter{}, nil
	}

	filter := &domain.SearchFilter{}
	var textParts []string
	i := 0

	for i < len(query) {
		// Skip whitespace
		if query[i] == ' ' || query[i] == '\t' {
			i++
			continue
		}

		start := i
		token, next, leadingQuote, perr := readToken(query, i)
		if perr != nil {
			return nil, perr
		}
		i = next

		// A token that opened with a quote is text, whatever is inside it, so
		// searching for the literal "foo:bar" still works.
		if leadingQuote {
			if token != "" {
				textParts = append(textParts, token)
			}
			continue
		}

		// A leading minus negates the token. Only tags support it: excluding a
		// level is the same as not including it, and the toggles express that
		// better than a negation would.
		negated := false
		if strings.HasPrefix(token, "-") && len(token) > 1 {
			negated = true
			token = token[1:]
		}

		// Check for field:value
		colonIdx := strings.Index(token, ":")
		if colonIdx > 0 {
			field := token[:colonIdx]
			value := token[colonIdx+1:]

			if !validFields[field] {
				return nil, &ParseError{Position: start, Message: fmt.Sprintf("unknown field '%s'. Valid fields: %s", field, validFieldList)}
			}
			if negated && field != "tag" {
				return nil, &ParseError{Position: start, Message: fmt.Sprintf("'%s' cannot be negated. Only tags can: -tag:noise", field)}
			}

			if value == "" {
				return nil, &ParseError{Position: start + colonIdx + 1, Message: fmt.Sprintf("missing value for field '%s'", field)}
			}

			switch field {
			case "level":
				if !validLevels[value] {
					return nil, &ParseError{Position: start, Message: fmt.Sprintf("invalid level '%s'. Valid: info, debug, warning, error, fatal, verbose, system", value)}
				}
				// Repeatable and de-duplicated, because the level toggles build
				// this by appending and a double click should not double it.
				if !contains(filter.Levels, value) {
					filter.Levels = append(filter.Levels, value)
				}
			case "tag":
				if negated {
					if !contains(filter.ExcludeTags, value) {
						filter.ExcludeTags = append(filter.ExcludeTags, value)
					}
				} else if !contains(filter.Tags, value) {
					filter.Tags = append(filter.Tags, value)
				}
			case "last":
				d, ok := windows[value]
				if !ok {
					return nil, &ParseError{Position: start, Message: fmt.Sprintf("unknown window 'last:%s'. Valid: 1h, 24h, 7d, 30d", value)}
				}
				since := time.Now().Add(-d)
				filter.Since = &since
				filter.SinceLabel = value
			case "is":
				if value == "network" {
					t := true
					filter.IsNetwork = &t
				} else {
					return nil, &ParseError{Position: start, Message: fmt.Sprintf("unknown filter 'is:%s'. Valid: is:network", value)}
				}
			case "fingerprint":
				filter.Fingerprint = &value
			case "session":
				uid, err := uuid.Parse(value)
				if err != nil {
					return nil, &ParseError{Position: start, Message: fmt.Sprintf("invalid session UUID: %s", value)}
				}
				filter.SessionID = &uid
			case "request":
				uid, err := uuid.Parse(value)
				if err != nil {
					return nil, &ParseError{Position: start, Message: fmt.Sprintf("invalid request UUID: %s", value)}
				}
				filter.RequestID = &uid

			// Session-scoped. Last one wins rather than accumulating: two
			// devices at once would match nothing, so repeating the field is
			// almost certainly someone correcting themselves.
			case "user":
				filter.Session.User = value
			case "device":
				filter.Session.Device = value
			case "os":
				filter.Session.OS = value
			case "app_version":
				filter.Session.AppVersion = value
			case "sdk_version":
				filter.Session.SDKVersion = value
			case "installation":
				filter.Session.Installation = value
			}
		} else {
			if negated {
				return nil, &ParseError{Position: start, Message: "only tags can be negated: -tag:noise"}
			}
			// Bare word — add to text query
			textParts = append(textParts, token)
		}
	}

	if len(textParts) > 0 {
		filter.TextQuery = strings.Join(textParts, " ")
	}

	return filter, nil
}

// readToken reads one token, ending at whitespace. A quoted run inside the
// token is consumed whole, so a field value may contain spaces:
//
//	fingerprint:"User {n} not found"
//
// which is how the Errors screen links to the occurrences of one group.
// Backslash escapes the next character inside quotes, so a fingerprint that
// contains a quote of its own still survives the round trip.
//
// leadingQuote reports whether the token opened with a quote, which is what
// tells free text apart from a field.
func readToken(q string, i int) (token string, next int, leadingQuote bool, err *ParseError) {
	start := i
	leadingQuote = q[i] == '"'

	var b strings.Builder
	for i < len(q) && q[i] != ' ' && q[i] != '\t' {
		if q[i] != '"' {
			b.WriteByte(q[i])
			i++
			continue
		}
		i++ // opening quote
		for {
			if i >= len(q) {
				return "", 0, false, &ParseError{Position: start, Message: "unclosed quote"}
			}
			if q[i] == '\\' && i+1 < len(q) {
				b.WriteByte(q[i+1])
				i += 2
				continue
			}
			if q[i] == '"' {
				i++
				break
			}
			b.WriteByte(q[i])
			i++
		}
	}
	return b.String(), i, leadingQuote, nil
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
