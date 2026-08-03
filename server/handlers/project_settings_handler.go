package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/internal/ports"
	"github.com/t0uh33d/code_scout/internal/services"
	"github.com/t0uh33d/code_scout/pkg/cslog"
	"github.com/t0uh33d/code_scout/pkg/utils"
	"github.com/t0uh33d/code_scout/server/middleware"
	"github.com/t0uh33d/code_scout/view"
)

type ProjectSettingsHandler struct {
	projectSvc ports.ProjectManager
	memberSvc  *services.MemberService
}

func NewProjectSettingsHandler(projectSvc ports.ProjectManager, memberSvc *services.MemberService) *ProjectSettingsHandler {
	return &ProjectSettingsHandler{projectSvc: projectSvc, memberSvc: memberSvc}
}

// SetAccess handles POST /project/{id}/settings/access — add someone to this
// project or change their level.
func (h *ProjectSettingsHandler) SetAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := middleware.ProjectIDFrom(ctx)

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	targetID, err := uuid.Parse(r.FormValue("user_id"))
	if err != nil {
		http.Error(w, "Invalid account", http.StatusBadRequest)
		return
	}

	level := domain.ProjectLevel(r.FormValue("level"))
	if _, err := h.memberSvc.SetProjectAccess(ctx, middleware.UserFrom(ctx), projectID, targetID, level); err != nil {
		h.renderAccess(w, r, projectID, err.Error())
		return
	}
	h.renderAccess(w, r, projectID, "")
}

// RemoveAccess handles POST /project/{id}/settings/access/remove.
func (h *ProjectSettingsHandler) RemoveAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := middleware.ProjectIDFrom(ctx)

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	targetID, err := uuid.Parse(r.FormValue("user_id"))
	if err != nil {
		http.Error(w, "Invalid account", http.StatusBadRequest)
		return
	}

	if _, err := h.memberSvc.RemoveProjectAccess(ctx, middleware.UserFrom(ctx), projectID, targetID); err != nil {
		h.renderAccess(w, r, projectID, err.Error())
		return
	}
	h.renderAccess(w, r, projectID, "")
}

// renderAccess re-renders the whole access panel, which is the target of both
// access actions. 200 even on failure: HTMX will not swap a non-2xx body, so an
// error rendered with 4xx would never reach the page.
func (h *ProjectSettingsHandler) renderAccess(w http.ResponseWriter, r *http.Request, projectID uuid.UUID, errMsg string) {
	ctx := r.Context()
	actor := middleware.UserFrom(ctx)

	members, _, err := h.memberSvc.ListProjectMembers(ctx, actor, projectID)
	if err != nil {
		members = nil
	}
	candidates, _ := h.memberSvc.ListAssignableAccounts(ctx, projectID)

	view.AccessPanel(view.AccessPanelData{
		ProjectID:  projectID,
		Members:    members,
		Candidates: candidates,
		CanManage:  middleware.ProjectAccessFrom(ctx).CanManage,
		ActorID:    actorID(actor),
		Error:      errMsg,
	}).Render(ctx, w)
}

func actorID(u *domain.User) uuid.UUID {
	if u == nil {
		return uuid.Nil
	}
	return u.ID
}

// projectFromRequest resolves and loads the project, writing the response
// itself when it cannot. Callers stop on a nil result.
func (h *ProjectSettingsHandler) projectFromRequest(w http.ResponseWriter, r *http.Request) *domain.Project {
	projectID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return nil
	}

	project, status, err := h.projectSvc.GetProject(r.Context(), projectID)
	if err != nil {
		http.Error(w, "Project not found", status)
		return nil
	}
	return project
}

// Settings renders GET /project/{id}/settings.
func (h *ProjectSettingsHandler) Settings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	project := h.projectFromRequest(w, r)
	if project == nil {
		return
	}

	// Only whether a secret exists, never the secret itself: the delivered page
	// must not contain it.
	_, _, secretErr := h.projectSvc.RevealSecret(ctx, project.ID)

	actor := middleware.UserFrom(ctx)
	access := middleware.ProjectAccessFrom(ctx)
	members, _, _ := h.memberSvc.ListProjectMembers(ctx, actor, project.ID)
	candidates, _ := h.memberSvc.ListAssignableAccounts(ctx, project.ID)

	data := view.SettingsData{
		User:      actor,
		Project:   project,
		HasSecret: secretErr == nil,
		CanManage: access.CanManage,
		CanDelete: access.CanDelete,
		Tab:       r.URL.Query().Get("tab"),
		BaseURL:   publicBaseURL(r),
		Access: view.AccessPanelData{
			ProjectID:  project.ID,
			Members:    members,
			Candidates: candidates,
			CanManage:  access.CanManage,
			ActorID:    actorID(actor),
		},
	}

	// Switching tabs asks for the same URL, so it stays linkable and reloadable.
	// htmx only wants the part that changes; a browser wants the whole page.
	if r.Header.Get("HX-Request") == "true" {
		view.SettingsBody(data).Render(ctx, w)
		return
	}
	view.SettingsPage(data).Render(ctx, w)
}

// UpdateGeneral handles POST /project/{id}/settings/general and answers with the
// whole form: saved, or the same values plus inline errors.
func (h *ProjectSettingsHandler) UpdateGeneral(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// -1 on a value that is not a number, rather than Atoi's 0. Zero is a real
	// setting that means "record nothing", so falling back to it would silently
	// switch a project off; -1 fails validation and says so.
	rate, convErr := strconv.Atoi(strings.TrimSpace(r.FormValue("session_sample_rate")))
	if convErr != nil {
		rate = -1
	}

	opts := &domain.UpdateProjectOpts{
		Name:              r.FormValue("name"),
		Description:       r.FormValue("description"),
		SessionSampleRate: &rate,
	}

	project, _, err := h.projectSvc.UpdateProject(ctx, projectID, opts)
	if err != nil {
		// 200 on purpose. HTMX does not swap the body of a non-2xx response, so
		// an error rendered with 400 would never reach the page.
		var appErr *utils.ErrorJson
		if errors.As(err, &appErr) && len(appErr.Errors) > 0 {
			view.GeneralForm(view.GeneralFormData{
				ProjectID: projectID, Name: opts.Name, Description: opts.Description,
				SessionSampleRate: rate, Errors: appErr.Errors,
			}).Render(ctx, w)
			return
		}
		cslog.L(ctx).WithError(err).Error("Failed to update project")
		view.GeneralForm(view.GeneralFormData{
			ProjectID: projectID, Name: opts.Name, Description: opts.Description,
			SessionSampleRate: rate,
			Errors:            []utils.FieldError{{Field: "form", Detail: "Could not save your changes. Try again."}},
		}).Render(ctx, w)
		return
	}

	// CreatedAt travels with every re-render, or the "Created" line would
	// disappear the first time someone saves.
	view.GeneralForm(view.GeneralFormData{
		ProjectID: projectID, Name: project.Name, Description: project.Description,
		SessionSampleRate: project.SessionSampleRate,
		CreatedAt:         project.CreatedAt, Saved: true,
	}).Render(ctx, w)
	// The sidebar heading lives outside this form, so it is updated
	// out-of-band in the same response.
	view.ShellNameOOB(project.Name).Render(ctx, w)
}

// RevealSecret handles GET /project/{id}/settings/secret. With ?hide=1 it
// returns the masked control without reading the secret at all.
func (h *ProjectSettingsHandler) RevealSecret(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}

	// The SDK tab asks for the snippet too, so the block you copy carries the
	// real key. Requested explicitly rather than always sent: an out-of-band
	// swap whose target is absent fails silently, and on the other tabs there
	// is no snippet to update.
	withSnippet := r.URL.Query().Get("snippet") != ""

	if r.URL.Query().Get("hide") != "" {
		view.SecretField(projectID, "", false).Render(ctx, w)
		if withSnippet {
			view.SetupSnippetOOB(projectID, "", publicBaseURL(r)).Render(ctx, w)
		}
		return
	}

	secret, _, err := h.projectSvc.RevealSecret(ctx, projectID)
	if err != nil {
		view.SecretField(projectID, "", false).Render(ctx, w)
		return
	}

	// Keep the one response that carries a live credential out of any cache.
	w.Header().Set("Cache-Control", "no-store")
	view.SecretField(projectID, secret, true).Render(ctx, w)
	if withSnippet {
		view.SetupSnippetOOB(projectID, secret, publicBaseURL(r)).Render(ctx, w)
	}
}

// ConfirmDialog handles GET /project/{id}/settings/confirm and builds the
// type-to-confirm modal for the requested action.
func (h *ProjectSettingsHandler) ConfirmDialog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	project := h.projectFromRequest(w, r)
	if project == nil {
		return
	}

	switch r.URL.Query().Get("action") {
	case "rotate":
		view.ConfirmModal(view.ConfirmDialog{
			Title: "Rotate the secret key",
			Body: "A new key is generated and the current one stops working immediately. " +
				"Every app still using the old key will fail to upload until you update its configuration.",
			Phrase:      "rotate",
			ConfirmText: "Rotate secret",
			PostURL:     fmt.Sprintf("/project/%s/settings/secret/rotate", project.ID),
			// The new key lands in the credentials section behind the dialog.
			Target: "#secret-field",
			Swap:   "outerHTML",
		}).Render(ctx, w)
	case "delete":
		view.ConfirmModal(view.ConfirmDialog{
			Title: "Delete this project",
			Body: fmt.Sprintf("%q and its credentials are removed immediately, and its logs stop being reachable. This cannot be undone.",
				project.Name),
			// The project's own name, so muscle memory cannot carry someone
			// through this dialog the way a fixed word would.
			Phrase:      project.Name,
			ConfirmText: "Delete permanently",
			Danger:      true,
			PostURL:     fmt.Sprintf("/project/%s/settings/delete", project.ID),
			Target:      "#confirm-error",
			Swap:        "innerHTML",
		}).Render(ctx, w)
	default:
		http.Error(w, "Unknown action", http.StatusBadRequest)
	}
}

// RotateSecret handles POST /project/{id}/settings/secret/rotate.
func (h *ProjectSettingsHandler) RotateSecret(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}

	secret, _, err := h.projectSvc.RotateSecret(ctx, projectID)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to rotate secret")
		view.SettingsError("Could not rotate the secret. Try again.").Render(ctx, w)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	// Two things in one response: the new key revealed once into the page, and
	// an empty dialog host that dismisses the modal.
	view.SecretField(projectID, secret, true).Render(ctx, w)
	view.ConfirmHostOOB().Render(ctx, w)
}

// DeleteProject handles POST /project/{id}/settings/delete.
func (h *ProjectSettingsHandler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	projectID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, "Invalid project ID", http.StatusBadRequest)
		return
	}

	if _, err := h.projectSvc.DeleteProject(ctx, projectID); err != nil {
		cslog.L(ctx).WithError(err).Error("Failed to delete project")
		view.SettingsError("Could not delete this project. Try again.").Render(ctx, w)
		return
	}

	// The page's subject no longer exists, so swapping anything into it would
	// leave a settings screen for a deleted project. HX-Redirect tells HTMX to
	// navigate the whole window instead.
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}
