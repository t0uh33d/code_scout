package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/ports"
)

const sessionCookieName = "cs_session"

// clearSessionCookie expires the cookie with the same flags it was written
// with. Secure follows the request's scheme for the same reason it does on the
// write side: a browser treats cs_session and Secure cs_session as two
// different cookies, so clearing one leaves the other in place.
//
// Kept here rather than shared with the handlers package because middleware
// cannot import handlers, and one small duplicated helper beats an import
// cycle or a third package holding four lines.
func clearSessionCookie(r *http.Request) *http.Cookie {
	secure := r.TLS != nil ||
		strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// changePasswordPath is the one place an account on a temporary password is
// allowed to reach. Both the GET and the POST live on it, so a single path
// comparison covers the whole screen.
const changePasswordPath = "/change-password"

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
				// Clear stale cookie. The flags have to match the ones it was
				// written with or the browser keeps the old cookie beside this
				// one: a Secure cs_session is a different cookie from a
				// non-Secure cs_session, and clearing the wrong one leaves a
				// dead token being resent on every request.
				http.SetCookie(w, clearSessionCookie(r))
				toLogin()
				return
			}

			// An account still on a temporary password gets nowhere else. The
			// check lives here rather than in each handler because "nowhere
			// else" has to mean every route, including ones added later.
			if user.MustChangePassword && r.URL.Path != changePasswordPath {
				if r.Header.Get("HX-Request") == "true" {
					w.Header().Set("HX-Redirect", changePasswordPath)
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				http.Redirect(w, r, changePasswordPath, http.StatusFound)
				return
			}

			// Everything downstream needs to know who this is: favourites are
			// per user, and the account menu shows their name.
			ctx := context.WithValue(r.Context(), userCtxKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
