package middleware

import (
	"net/http"

	"github.com/getcodescout/code_scout/internal/domain"
)

// RequireSuperAdmin gates instance-wide settings. Changing them affects every
// project and every account, so it sits with the role that already owns the
// instance rather than with anyone who can create projects.
//
// Must be mounted under RequireSession, which is what puts the user in context.
func RequireSuperAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFrom(r.Context())
		if user == nil || user.Role != domain.RoleSuperAdmin {
			// 404 rather than 403, matching the project guards: someone who may
			// not use a screen has no need to learn it exists.
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
