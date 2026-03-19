package search

import (
	"fmt"
	"strings"

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
	"level": true, "tag": true, "is": true, "session": true, "request": true,
}

// Parse converts a search query string into a SearchFilter.
// Supports: level:error, tag:auth, is:network, session:UUID, request:UUID, "quoted text", bare words.
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

		// Check for field:value
		colonIdx := strings.Index(token, ":")
		if colonIdx > 0 {
			field := token[:colonIdx]
			value := token[colonIdx+1:]

			if !validFields[field] {
				return nil, &ParseError{Position: start, Message: fmt.Sprintf("unknown field '%s'. Valid fields: level, tag, is, session, request", field)}
			}

			if value == "" {
				return nil, &ParseError{Position: start + colonIdx + 1, Message: fmt.Sprintf("missing value for field '%s'", field)}
			}

			switch field {
			case "level":
				if !validLevels[value] {
					return nil, &ParseError{Position: start, Message: fmt.Sprintf("invalid level '%s'. Valid: info, debug, warning, error, fatal, verbose, system", value)}
				}
				filter.Level = &value
			case "tag":
				filter.Tags = append(filter.Tags, value)
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
			// Bare word — add to text query
			textParts = append(textParts, token)
		}
	}

	if len(textParts) > 0 {
		filter.TextQuery = strings.Join(textParts, " ")
	}

	return filter, nil
}
