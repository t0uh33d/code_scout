package mcptools

import (
	"net/http"

	"github.com/getcodescout/code_scout/app"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer builds the MCP server with every tool registered. Exported apart
// from the HTTP handler so tests can drive it over an in-memory transport.
func NewServer(d Deps) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "code-scout",
		Version: app.Version,
	}, nil)

	d.addProjectTools(s)
	d.addLogTools(s)
	d.addSessionTools(s)
	d.addNetworkTools(s)
	d.addLiveTools(s)
	d.addLiveDBTools(s)

	return s
}

// NewHTTPHandler mounts the server for the streamable HTTP transport, mounted
// in routes.go behind RequirePersonalToken.
//
// Stateless: every POST is a complete exchange, nothing is held between
// requests, and no Mcp-Session-Id bookkeeping exists to leak. JSONResponse:
// answers are one application/json body, never a hanging SSE stream, so the
// server's 30 second WriteTimeout can never cut a response off.
func NewHTTPHandler(d Deps) http.Handler {
	server := NewServer(d)
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)
}
