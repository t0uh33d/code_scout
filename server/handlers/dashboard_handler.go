package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	confs "github.com/getcodescout/code_scout/conf"
	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/ports"
	"github.com/getcodescout/code_scout/internal/services"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"github.com/getcodescout/code_scout/pkg/utils"
	"github.com/getcodescout/code_scout/server/middleware"
	"github.com/getcodescout/code_scout/view"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type DashboardHandler struct {
	projectSvc ports.ProjectManager
	memberSvc  *services.MemberService
}

func NewDashboardHandler(projectSvc ports.ProjectManager, memberSvc *services.MemberService) *DashboardHandler {
	return &DashboardHandler{projectSvc: projectSvc, memberSvc: memberSvc}
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
	opts.ScopeToUser(middleware.UserFrom(r.Context()))

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

// ToggleFavorite handles POST /project/{id}/favorite and returns the
// re-rendered star, which is the button that was pressed.
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
	isJSON := contentType == "application/json"

	if isJSON {
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

	actor := middleware.UserFrom(ctx)
	if actor == nil || !actor.CanCreateProjects() {
		http.Error(w, "You cannot create projects", http.StatusForbidden)
		return
	}
	// Never from the form: the creator is whoever is signed in, and they become
	// the project's first maintainer.
	opts.CreatedBy = actor.ID

	// The same list the Access step offered. Recomputing it here is what turns
	// the ids in the form back into a decision the server made: anything not on
	// this list is dropped, so a hand-edited form cannot add a super admin or a
	// made-up account.
	candidates := h.candidates(ctx, actor)
	if !isJSON {
		opts.Members = parseMembers(r, candidates)
	}

	details, _, err := h.projectSvc.CreateProject(ctx, opts)
	if err != nil {
		log.WithError(err).Error("Failed to create project")
		if isJSON {
			RespondError(w, err)
			return
		}
		// Back to step 1 with the answers intact. 200 on purpose: HTMX drops the
		// body of a non-2xx response, so an error rendered with 400 would leave
		// the sheet frozen on a button that did nothing.
		draft := view.WizardDraft{Name: opts.Name, Description: opts.Description, Candidates: candidates}
		var appErr *utils.ErrorJson
		if errors.As(err, &appErr) && len(appErr.Errors) > 0 {
			draft.Errors = appErr.Errors
		} else {
			draft.Errors = []utils.FieldError{{Field: "name", Detail: "Could not create the project. Try again."}}
		}
		view.WizardDetails(draft).Render(ctx, w)
		return
	}

	// The credentials panel goes first: this response carries the only copy of
	// the plaintext secret the user will ever be shown.
	// The wizard's final step goes to the modal, and the refreshed page goes
	// out-of-band to the list behind it, so one response updates both.
	if err := view.WizardConnect(details, publicBaseURL(r), len(candidates) > 0).Render(ctx, w); err != nil {
		log.WithError(err).Error("Failed to render wizard step")
		return
	}

	listing := domain.ProjectListOpts{Page: 1, PageSize: 12}
	listing.ScopeToUser(middleware.UserFrom(ctx))
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

// NewProjectWizard handles GET and POST /dashboard/projects/new — the first
// step. GET is the modal opening, fetched fresh each time so it never reopens on
// a stale later step. POST is the Access step's Back button, which sends the
// draft back so the fields are not lost.
func (h *DashboardHandler) NewProjectWizard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	draft := view.WizardDraft{Candidates: h.candidates(ctx, middleware.UserFrom(ctx))}
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err == nil {
			draft.Name = r.FormValue("name")
			draft.Description = r.FormValue("description")
		}
	}
	if err := view.WizardDetails(draft).Render(ctx, w); err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to render project wizard")
	}
}

// ProjectWizardAccess handles POST /dashboard/projects/access — step 2. It
// renders only; the project is created when this step is submitted.
func (h *DashboardHandler) ProjectWizardAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	actor := middleware.UserFrom(ctx)
	if actor == nil || !actor.CanCreateProjects() {
		http.Error(w, "You cannot create projects", http.StatusForbidden)
		return
	}

	draft := view.WizardDraft{
		Name:        r.FormValue("name"),
		Description: r.FormValue("description"),
		Candidates:  h.candidates(ctx, actor),
	}
	// Nobody to add: skip straight to creating, which is what step 1 would have
	// done had the list been empty when it rendered.
	if len(draft.Candidates) == 0 {
		h.CreateProject(w, r)
		return
	}
	if err := view.WizardAccess(draft).Render(ctx, w); err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to render access step")
	}
}

// candidates lists who the Access step may offer. A failure returns an empty
// list rather than an error: the wizard then behaves like a solo instance,
// which loses a convenience but never blocks creating a project.
func (h *DashboardHandler) candidates(ctx context.Context, actor *domain.User) []domain.User {
	if h.memberSvc == nil || actor == nil {
		return nil
	}
	users, err := h.memberSvc.ListCandidatesForNewProject(ctx, actor)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to list access candidates")
		return nil
	}
	return users
}

// parseMembers turns the ticked boxes into assignments. Each id is checked
// against the offered list, so a hand-edited form cannot add an account the
// wizard never offered. An unrecognised level falls back to viewer, the lesser
// of the two.
func parseMembers(r *http.Request, candidates []domain.User) []domain.ProjectMemberInput {
	if len(candidates) == 0 {
		return nil
	}
	offered := make(map[uuid.UUID]bool, len(candidates))
	for _, c := range candidates {
		offered[c.ID] = true
	}

	out := make([]domain.ProjectMemberInput, 0, len(r.Form["member"]))
	for _, raw := range r.Form["member"] {
		id, err := uuid.Parse(raw)
		if err != nil || !offered[id] {
			continue
		}
		level := domain.ProjectLevel(r.FormValue("level_" + raw))
		if !level.Valid() {
			level = domain.LevelViewer
		}
		out = append(out, domain.ProjectMemberInput{UserID: id, Level: level})
	}
	return out
}
