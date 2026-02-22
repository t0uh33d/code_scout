package middleware

import (
	"net/http"

	"github.com/t0uh33d/code_scout/internal/ports"
)

const sessionCookieName = "cs_session"

// RequireSession is middleware for web page routes that redirects to /login
// if the user does not have a valid session cookie.
func RequireSession(authSvc ports.AuthManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(sessionCookieName)
			if err != nil || cookie.Value == "" {
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}

			_, _, err = authSvc.ValidateSession(r.Context(), cookie.Value)
			if err != nil {
				// Clear stale cookie
				http.SetCookie(w, &http.Cookie{
					Name:     sessionCookieName,
					Value:    "",
					Path:     "/",
					MaxAge:   -1,
					HttpOnly: true,
				})
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
