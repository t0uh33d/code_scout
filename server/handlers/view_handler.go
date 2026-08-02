package handlers

import (
	"context"
	"net/http"

	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/internal/ports"
	"github.com/t0uh33d/code_scout/pkg/cslog"
	"github.com/t0uh33d/code_scout/server/middleware"
	"github.com/t0uh33d/code_scout/view"
)

type ViewHandler struct {
	authSvc    ports.AuthManager
	projectSvc ports.ProjectManager
}

func NewViewHandler(authSvc ports.AuthManager, projectSvc ports.ProjectManager) *ViewHandler {
	return &ViewHandler{authSvc: authSvc, projectSvc: projectSvc}
}

// Dashboard renders the projects dashboard (GET /)
func (h *ViewHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user := middleware.UserFrom(ctx)

	opts := domain.ProjectListOpts{Page: 1, PageSize: 12}
	opts.ScopeToUser(user)

	result, _, err := h.projectSvc.ListProjects(ctx, opts)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to list projects for dashboard")
		result = &domain.ProjectListResult{}
	}

	data := view.DashboardData{
		Projects: result,
		Search:   "",
		Filter:   "all",
		User:     user,
	}

	// Render against the request context, not a fresh background one, or the
	// request-scoped logger and any cancellation are lost.
	view.Dashboard(data).Render(ctx, w)
}

// Login renders the auth page.
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
