package handlers

import (
	"net/http"

	"gorm.io/gorm"

	dbadapter "github.com/getcodescout/code_scout/internal/adapters/db"
)

type HealthHandler struct {
	db     *gorm.DB
	commit string
}

func NewHealthHandler(db *gorm.DB, commit string) *HealthHandler {
	return &HealthHandler{db: db, commit: commit}
}

// Health reports whether the instance can serve traffic. Container
// orchestrators poll this, so it stays unauthenticated and cheap, and it
// returns 503 when the database is unreachable so an unhealthy pod is
// taken out of rotation instead of serving errors.
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	status, dbState, code := "ok", "ok", http.StatusOK

	if h.db == nil {
		status, dbState, code = "degraded", "not configured", http.StatusServiceUnavailable
	} else if err := dbadapter.Ping(h.db); err != nil {
		status, dbState, code = "degraded", "unreachable", http.StatusServiceUnavailable
	}

	RespondJSON(w, code, map[string]string{
		"status": status,
		"db":     dbState,
		"commit": h.commit,
	})
}
