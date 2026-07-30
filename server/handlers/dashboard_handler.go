package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/internal/ports"
	"github.com/t0uh33d/code_scout/pkg/cslog"
	"github.com/t0uh33d/code_scout/view"
)

type DashboardHandler struct {
	projectSvc ports.ProjectManager
}

func NewDashboardHandler(projectSvc ports.ProjectManager) *DashboardHandler {
	return &DashboardHandler{projectSvc: projectSvc}
}

// ProjectsGrid handles GET /dashboard/projects — returns the project grid partial for HTMX.
func (h *DashboardHandler) ProjectsGrid(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	opts := domain.ProjectListOpts{
		Search:   r.URL.Query().Get("search"),
		Page:     1,
		PageSize: 12,
	}

	if p := r.URL.Query().Get("page"); p != "" {
		if pn, err := strconv.Atoi(p); err == nil && pn > 0 {
			opts.Page = pn
		}
	}

	result, _, err := h.projectSvc.ListProjects(ctx, opts)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to list projects")
		http.Error(w, "Failed to load projects", http.StatusInternalServerError)
		return
	}

	c := view.ProjectGrid(result)
	c.Render(r.Context(), w)
}

// CreateProject handles POST /dashboard/projects — creates a project via form, returns updated grid partial.
func (h *DashboardHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := cslog.L(ctx)

	contentType := r.Header.Get("Content-Type")
	opts := &domain.CreateProjectOpts{}

	if contentType == "application/json" {
		if err := json.NewDecoder(r.Body).Decode(opts); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		opts.Name = r.FormValue("name")
		opts.Description = r.FormValue("description")
	}

	details, _, err := h.projectSvc.CreateProject(ctx, opts)
	if err != nil {
		log.WithError(err).Error("Failed to create project")
		http.Error(w, "Failed to create project", http.StatusInternalServerError)
		return
	}

	// The credentials panel goes first: this response carries the only copy of
	// the plaintext secret the user will ever be shown.
	result, _, _ := h.projectSvc.ListProjects(ctx, domain.ProjectListOpts{Page: 1, PageSize: 12})
	if err := view.NewProjectCredentials(details).Render(ctx, w); err != nil {
		log.WithError(err).Error("Failed to render credentials")
		return
	}
	if err := view.ProjectGrid(result).Render(ctx, w); err != nil {
		log.WithError(err).Error("Failed to render project grid")
	}
}
