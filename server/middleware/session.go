package middleware

import (
	"context"
	"net/http"

	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/internal/ports"
)

const sessionCookieName = "cs_session"

type ctxKey string

const userCtxKey ctxKey = "cs_user"

// UserFrom returns the signed-in user, or nil if the request did not pass
// through RequireSession.
func UserFrom(ctx context.Context) *domain.User {
	user, _ := ctx.Value(userCtxKey).(*domain.User)
	return user
}

// RequireSession is middleware for web page routes that redirects to /login
// if the user does not have a valid session cookie.
func RequireSession(authSvc ports.AuthManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// A plain 302 breaks HTMX partials: the XHR follows it silently and
			// htmx swaps the login PAGE into whatever fragment made the request.
			// HX-Redirect tells htmx to navigate the whole window instead.
			toLogin := func() {
				if r.Header.Get("HX-Request") == "true" {
					w.Header().Set("HX-Redirect", "/login")
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				http.Redirect(w, r, "/login", http.StatusFound)
			}

			cookie, err := r.Cookie(sessionCookieName)
			if err != nil || cookie.Value == "" {
				toLogin()
				return
			}

			user, _, err := authSvc.ValidateSession(r.Context(), cookie.Value)
			if err != nil {
				// Clear stale cookie
				http.SetCookie(w, &http.Cookie{
					Name:     sessionCookieName,
					Value:    "",
					Path:     "/",
					MaxAge:   -1,
					HttpOnly: true,
				})
				toLogin()
				return
			}

			// Everything downstream needs to know who this is: favourites are
			// per user, and the account menu shows their name.
			ctx := context.WithValue(r.Context(), userCtxKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
