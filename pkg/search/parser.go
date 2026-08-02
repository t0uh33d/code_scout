package search

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
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
}

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

		// Quoted string
		if query[i] == '"' {
			start := i
			i++ // skip opening quote
			end := strings.Index(query[i:], "\"")
			if end == -1 {
				return nil, &ParseError{Position: start, Message: "unclosed quote"}
			}
			textParts = append(textParts, query[i:i+end])
			i += end + 1 // skip closing quote
			continue
		}

		// Read a token (until space or end)
		start := i
		for i < len(query) && query[i] != ' ' && query[i] != '\t' {
			i++
		}
		token := query[start:i]

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
				return nil, &ParseError{Position: start, Message: fmt.Sprintf("unknown field '%s'. Valid fields: level, tag, is, last, session, request", field)}
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

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
