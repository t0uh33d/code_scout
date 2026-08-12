package mcptools

import (
	"context"
	"errors"
	"strings"

	"github.com/getcodescout/code_scout/server/middleware"
	"github.com/google/uuid"
)

// errNotFoundFor builds the one answer a missing thing ever gets. For
// projects it covers a malformed id, an id that does not exist, and a project
// the caller may not read alike, so the three are byte-identical on the wire:
// the web router's 404-never-403 rule, kept by construction rather than care.
func errNotFoundFor(what string) error {
	return errors.New(what + " not found")
}

var errProjectNotFound = errNotFoundFor("project")

// requireProject is the only place tool access is decided. Every tool calls
// it first, the way every project page sits behind RequireProjectAccess.
func (d Deps) requireProject(ctx context.Context, rawID string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(rawID))
	if err != nil {
		return uuid.Nil, errProjectNotFound
	}
	access, err := d.Access.ResolveAccess(ctx, middleware.UserFrom(ctx), id)
	if err != nil || !access.CanRead {
		return uuid.Nil, errProjectNotFound
	}
	return id, nil
}
