package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/ports"
	"github.com/getcodescout/code_scout/pkg/utils"
	"github.com/getcodescout/code_scout/server/middleware"
	"github.com/getcodescout/code_scout/view"
)

const sessionCookieName = "cs_session"

// overHTTPS reports whether this request reached us on TLS, directly or through
// a proxy that terminated it.
//
// It decides the Secure flag on the session cookie. Set unconditionally, the
// cookie would vanish on a plain-HTTP dev instance and on the LAN address a
// phone uses during setup; never set, a single downgraded request puts a live
// session token on the wire in cleartext.
//
// X-Forwarded-Proto is trusted here because a browser cannot set it: it is
// stripped and rewritten by any proxy worth deploying, and a client that talks
// to the server directly is not on TLS anyway, so forging it only ever costs
// the forger their own cookie.
func overHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// sessionCookie is the one place the cookie's flags are decided, so a new call
// site cannot quietly ship without Secure or HttpOnly. value "" with maxAge -1
// is the clear-on-logout form.
func sessionCookie(r *http.Request, value string, expires time.Time, maxAge int) *http.Cookie {
	c := &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   overHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	}
	if maxAge != 0 {
		c.MaxAge = maxAge
	} else {
		c.Expires = expires
	}
	return c
}

type AuthHandler struct {
	authSvc ports.AuthManager
}

func NewAuthHandler(authSvc ports.AuthManager) *AuthHandler {
	return &AuthHandler{authSvc: authSvc}
}

// Submit handles POST /api/auth/submit — login or register based on first-run state.
func (h *AuthHandler) Submit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	opts := &domain.AuthOpts{
		Name:            r.FormValue("name"),
		Email:           r.FormValue("email"),
		Password:        r.FormValue("password"),
		ConfirmPassword: r.FormValue("confirm_password"),
	}

	token, _, status, err := h.authSvc.LoginOrRegister(ctx, opts)
	if err != nil {
		// Re-render login page with error — show user-friendly message, not internal codes
		isFirst, _ := h.authSvc.IsFirstRun(ctx)
		errMsg := "Something went wrong. Please try again."
		if errJSON, ok := err.(*utils.ErrorJson); ok {
			errMsg = errJSON.Message
		}
		c := view.Login(isFirst, errMsg)
		w.WriteHeader(status)
		c.Render(r.Context(), w)
		return
	}

	// Set session cookie
	http.SetCookie(w, sessionCookie(r, token, time.Now().Add(30*24*time.Hour), 0))

	// Redirect to dashboard
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ChangePasswordPage handles GET /change-password.
func (h *AuthHandler) ChangePasswordPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFrom(r.Context())
	view.ChangePassword(view.ChangePasswordData{
		User:   user,
		Forced: user != nil && user.MustChangePassword,
	}).Render(r.Context(), w)
}

// ChangePassword handles POST /change-password. On success the old session is
// gone, so the response has to carry the replacement cookie before redirecting,
// or the next request would land back on the login page.
func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := middleware.UserFrom(ctx)

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// The same POST serves two screens: the standalone forced-change page,
	// and the Password tab on /account, which marks itself with from=account.
	// A forced account always gets the standalone page whatever the form
	// says, because it may not be anywhere else.
	fromAccount := r.FormValue("from") == "account" && user != nil && !user.MustChangePassword

	token, status, err := h.authSvc.ChangePassword(ctx, user,
		r.FormValue("current_password"), r.FormValue("password"), r.FormValue("confirm_password"))
	if err != nil {
		errs := fieldErrorsOr(err, "password", "Could not save the new password. Try again.")
		w.WriteHeader(status)
		if fromAccount {
			view.AccountPage(view.AccountData{User: user, Tab: "password", PasswordErrors: errs}).Render(ctx, w)
			return
		}
		view.ChangePassword(view.ChangePasswordData{
			User: user, Forced: user != nil && user.MustChangePassword, Errors: errs,
		}).Render(ctx, w)
		return
	}

	http.SetCookie(w, sessionCookie(r, token, time.Now().Add(30*24*time.Hour), 0))
	// Fixed strings only — the form names where it came from, it does not
	// carry a URL, so this cannot become an open redirect.
	if fromAccount {
		http.Redirect(w, r, "/account?tab=password&saved=1", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Logout handles POST /api/auth/logout — clears session cookie and deletes DB session.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	cookie, err := r.Cookie(sessionCookieName)
	if err == nil && cookie.Value != "" {
		_, _ = h.authSvc.Logout(ctx, cookie.Value)
	}

	// Clear the cookie. Through the same helper as the writes: a browser only
	// replaces a cookie when the flags match, so a clear that omits Secure
	// leaves the Secure one sitting there on an HTTPS instance.
	http.SetCookie(w, sessionCookie(r, "", time.Unix(0, 0), -1))

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
