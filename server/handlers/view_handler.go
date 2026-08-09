package handlers

import (
	"context"
	"net/http"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/ports"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"github.com/getcodescout/code_scout/server/middleware"
	"github.com/getcodescout/code_scout/view"
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

	// The tab is in the query string, so the favourites view is a URL you can
	// send someone or bookmark. Switching tabs is an htmx swap against
	// /dashboard/projects/list, which answers a fragment — landing on this page
	// is the only place the tab can be restored from.
	filter := "all"
	if r.URL.Query().Get("filter") == "favorites" {
		filter = "favorites"
	}

	opts := domain.ProjectListOpts{Page: 1, PageSize: 12, FavoritesOnly: filter == "favorites"}
	opts.ScopeToUser(user)

	result, _, err := h.projectSvc.ListProjects(ctx, opts)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to list projects for dashboard")
		result = &domain.ProjectListResult{}
	}

	data := view.DashboardData{
		Projects: result,
		Search:   "",
		Filter:   filter,
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
