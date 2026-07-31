package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	confs "github.com/t0uh33d/code_scout/conf"
	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/server/middleware"
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

// listOpts reads the shared filter/search/page query params.
func listOpts(r *http.Request) (domain.ProjectListOpts, string) {
	filter := r.URL.Query().Get("filter")

	opts := domain.ProjectListOpts{
		Search:        r.URL.Query().Get("search"),
		Page:          1,
		PageSize:      12,
		FavoritesOnly: filter == "favorites",
	}
	if user := middleware.UserFrom(r.Context()); user != nil {
		opts.UserID = user.ID
	}

	if p := r.URL.Query().Get("page"); p != "" {
		if pn, err := strconv.Atoi(p); err == nil && pn > 0 {
			opts.Page = pn
		}
	}

	return opts, filter
}

// ProjectsGrid handles GET /dashboard/projects — the grid alone, which is what
// searching and paging replace.
func (h *DashboardHandler) ProjectsGrid(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	opts, filter := listOpts(r)

	result, _, err := h.projectSvc.ListProjects(ctx, opts)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to list projects")
		http.Error(w, "Failed to load projects", http.StatusInternalServerError)
		return
	}

	// The filter and search travel back with the grid so the links inside it
	// (pagination) stay inside the current tab and query.
	view.ProjectGrid(result, filter, opts.Search).Render(ctx, w)
}

// ProjectsListPartial handles GET /dashboard/projects/list — the toolbar and the
// grid together, which is what switching tabs replaces so the active tab moves.
func (h *DashboardHandler) ProjectsListPartial(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	opts, filter := listOpts(r)

	result, _, err := h.projectSvc.ListProjects(ctx, opts)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to list projects")
		http.Error(w, "Failed to load projects", http.StatusInternalServerError)
		return
	}

	view.ProjectsList(view.DashboardData{
		Projects: result,
		Search:   opts.Search,
		Filter:   filter,
	}).Render(ctx, w)
}

// ToggleFavorite handles POST /dashboard/projects/{id}/favorite and returns the
// re-rendered star.
func (h *DashboardHandler) ToggleFavorite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user := middleware.UserFrom(ctx)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	projectID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}

	isFavorite, status, err := h.projectSvc.ToggleFavorite(ctx, user.ID, projectID)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to toggle favorite")
		http.Error(w, "Failed to update favorite", status)
		return
	}

	view.FavoriteStar(projectID, isFavorite).Render(ctx, w)
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
	// The wizard's final step goes to the modal, and the refreshed page goes
	// out-of-band to the list behind it, so one response updates both.
	if err := view.WizardConnect(details, publicBaseURL(r)).Render(ctx, w); err != nil {
		log.WithError(err).Error("Failed to render wizard step")
		return
	}

	listing := domain.ProjectListOpts{Page: 1, PageSize: 12}
	if user := middleware.UserFrom(ctx); user != nil {
		listing.UserID = user.ID
	}
	result, _, listErr := h.projectSvc.ListProjects(ctx, listing)
	if listErr != nil {
		// The create succeeded and the modal already carries the credentials.
		// Skipping the OOB refresh leaves the page one project stale, which
		// beats swapping a populated grid for the empty state because a
		// follow-up read blipped.
		log.WithError(listErr).Error("Skipping projects refresh after create")
		return
	}
	if err := view.ProjectsSectionOOB(view.DashboardData{Projects: result, Filter: "all"}).Render(ctx, w); err != nil {
		log.WithError(err).Error("Failed to render projects section")
	}
}

// publicBaseURL is the address a developer's app should send logs to.
//
// An explicitly configured value always wins. Otherwise it falls back to the
// host the operator reached this page on, which is the one address known to
// actually route here: browse the dashboard on a LAN IP and the snippet hands
// out that LAN IP, browse it on a domain and the snippet hands out the domain.
func publicBaseURL(r *http.Request) string {
	if confs.Conf.PublicBaseURL != "" {
		return confs.Conf.PublicBaseURL
	}
	scheme := "http"
	// Set by a TLS-terminating proxy. Only the scheme is taken from a header;
	// the host comes from the request line either way.
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" {
		scheme = "https"
	} else if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// NewProjectWizard handles GET /dashboard/projects/new — the first step, fetched
// fresh each time the modal opens so it never reopens on a stale later step.
func (h *DashboardHandler) NewProjectWizard(w http.ResponseWriter, r *http.Request) {
	if err := view.WizardDetails().Render(r.Context(), w); err != nil {
		cslog.L(r.Context()).WithError(err).Error("Failed to render project wizard")
	}
}
