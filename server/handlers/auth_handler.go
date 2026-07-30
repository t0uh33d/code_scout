package handlers

import (
	"net/http"
	"time"

	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/internal/ports"
	"github.com/t0uh33d/code_scout/pkg/utils"
	"github.com/t0uh33d/code_scout/view"
)

const sessionCookieName = "cs_session"

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
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Expires:  time.Now().Add(30 * 24 * time.Hour),
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect to dashboard
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// Logout handles POST /api/auth/logout — clears session cookie and deletes DB session.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	cookie, err := r.Cookie(sessionCookieName)
	if err == nil && cookie.Value != "" {
		_, _ = h.authSvc.Logout(ctx, cookie.Value)
	}

	// Clear the cookie
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
