package view

import (
	"sync"

	"github.com/getcodescout/code_scout/internal/domain"
)

// What the instance last learned about newer releases, held here for the same
// reason the timezone is: it is one instance-wide fact that a footer on every
// page wants to read, and threading it through every page's data struct would
// touch far more code while still ending up with a single source.
//
// The mutex is what makes it safe for the nightly check to write while requests
// are rendering.
var (
	updateMu    sync.RWMutex
	updateState domain.VersionState
)

// SetUpdateState is called by the version check, on the schedule and on demand.
func SetUpdateState(s domain.VersionState) {
	updateMu.Lock()
	updateState = s
	updateMu.Unlock()
}

// UpdateState is what the dashboard currently believes about newer releases.
// The zero value means nothing has been learned yet, which renders as nothing
// at all rather than as "up to date" — those are different claims.
func UpdateState() domain.VersionState {
	updateMu.RLock()
	defer updateMu.RUnlock()
	return updateState
}
