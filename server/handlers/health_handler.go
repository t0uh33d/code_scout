package handlers

import (
	"net/http"

	"gorm.io/gorm"

	"github.com/getcodescout/code_scout/app"
	dbadapter "github.com/getcodescout/code_scout/internal/adapters/db"
)

type HealthHandler struct {
	db *gorm.DB
}

// The build identity is read from app rather than injected. Everything else
// this server constructs is passed in so it can be swapped in a test, but a
// linker-set constant has nothing to swap it for, and threading it through
// ServerOpts only created somewhere for it to go stale.
func NewHealthHandler(db *gorm.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

// Health reports whether the instance can serve traffic. Container
// orchestrators poll this, so it stays unauthenticated and cheap, and it
// returns 503 when the database is unreachable so an unhealthy pod is
// taken out of rotation instead of serving errors.
//
// It reports the version unauthenticated on purpose. "What are you running?"
// is the first question asked about a misbehaving instance, and needing a
// login to answer it means the answer arrives a day later.
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	status, dbState, code := "ok", "ok", http.StatusOK

	if h.db == nil {
		status, dbState, code = "degraded", "not configured", http.StatusServiceUnavailable
	} else if err := dbadapter.Ping(h.db); err != nil {
		status, dbState, code = "degraded", "unreachable", http.StatusServiceUnavailable
	}

	RespondJSON(w, code, map[string]string{
		"status":  status,
		"db":      dbState,
		"version": app.Version,
		"commit":  app.Commit,
	})
}
