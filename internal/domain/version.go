package domain

import "time"

// VersionState is what the instance last learned about releases newer than
// itself. It is a cache, not a setting: it has no row anywhere, and a restart
// throws it away and asks again.
type VersionState struct {
	// Latest is the newest released version, without the "v". Empty until a
	// check has succeeded at least once, which is also what an instance with no
	// route to the internet looks like forever.
	Latest string

	// URL is that release's page, so the badge can link somewhere useful.
	URL string

	// CheckedAt is when the last attempt finished, successful or not. Shown so
	// an operator can tell "no update" from "has not managed to ask".
	CheckedAt time.Time

	// Behind is whether Latest is genuinely newer than this build. It is stored
	// rather than recomputed at render because the comparison is semver, not
	// string inequality, and every template that showed a badge would otherwise
	// have to know that.
	Behind bool

	// Err is why the last check failed, for the settings card. Kept as a string
	// because it is displayed and never inspected, and because a stored error
	// tempts callers into branching on its type.
	Err string
}

// UpdateCheckURL is the GitHub API endpoint the check calls.
//
// It is the releases endpoint rather than the tags one: a tag can exist before
// its release notes do, and telling somebody to upgrade to a version with no
// notes gives them nothing to read before deciding.
const UpdateCheckURL = "https://api.github.com/repos/getcodescout/code_scout/releases/latest"
