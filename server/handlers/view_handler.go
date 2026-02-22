package handlers

import (
	"context"
	"net/http"

	"github.com/t0uh33d/code_scout/internal/ports"
	"github.com/t0uh33d/code_scout/view"
)

type ViewHandler struct {
	authSvc ports.AuthManager
}

func NewViewHandler(authSvc ports.AuthManager) *ViewHandler {
	return &ViewHandler{authSvc: authSvc}
}

// BaseLayout (Dashboard) — session protection is handled by RequireSession middleware,
// so by the time we get here the user is authenticated.
func (h *ViewHandler) BaseLayout(w http.ResponseWriter, r *http.Request) {
	c := view.BaseLayout("Code Scout")
	c.Render(context.Background(), w)
}

// Login renders the auth page. If no session, determine first-run state.
func (h *ViewHandler) Login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// If already authenticated, redirect to dashboard
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil && cookie.Value != "" {
		_, _, err = h.authSvc.ValidateSession(ctx, cookie.Value)
		if err == nil {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
	}

	isFirst, _ := h.authSvc.IsFirstRun(ctx)
	c := view.Login(isFirst, "")
	c.Render(context.Background(), w)
}
