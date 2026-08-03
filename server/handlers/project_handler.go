package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/ports"
	"github.com/getcodescout/code_scout/internal/services"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"github.com/getcodescout/code_scout/pkg/utils"
	"github.com/getcodescout/code_scout/server/middleware"
)

type ProjectHandler struct {
	svc       ports.ProjectManager
	memberSvc *services.MemberService
}

func NewProjectHandler(svc ports.ProjectManager, memberSvc *services.MemberService) *ProjectHandler {
	return &ProjectHandler{svc: svc, memberSvc: memberSvc}
}

func (h *ProjectHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := cslog.L(ctx)

	opts := &domain.CreateProjectOpts{}
	err := json.NewDecoder(r.Body).Decode(opts)
	if err != nil {
		log.WithError(err).Error("Failed to decode request body")
		RespondError(w, utils.NewError(
			nil, domain.INVALID_REQUEST_DATA_ERR_CODE,
			err,
		))
		return
	}

	// This route sits on the session-protected router, so there is always a
	// signed-in account. They become the project's first maintainer, or the
	// project would exist with nobody able to see it.
	actor := middleware.UserFrom(ctx)
	if actor == nil || !actor.CanCreateProjects() {
		RespondError(w, utils.NewError(nil, domain.UNAUTHORIZED, errors.New("You cannot create projects")))
		return
	}
	opts.CreatedBy = actor.ID

	resp, _, err := h.svc.CreateProject(ctx, opts)
	if err != nil {
		log.WithError(err).Error("Failed to create project")
		RespondError(w, err)
		return
	}

	RespondJSON(w, http.StatusOK, resp)
}

func (h *ProjectHandler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := cslog.L(ctx)

	projectID, err := utils.GetUUIDParam(r, "project_id")
	if err != nil {
		log.WithError(err).Error("Invalid project ID")
		RespondError(w, utils.NewError(
			nil, domain.INVALID_REQUEST_DATA_ERR_CODE,
			err,
		))
		return
	}

	// This route predates project access control and carries {project_id}, so it
	// does not match the /project/{id} subrouter that guards the browser delete.
	// Without this check any signed-in account could destroy any project by id,
	// routing straight around the "maintainer AND admin" rule.
	access, accessErr := h.memberSvc.ResolveAccess(ctx, middleware.UserFrom(ctx), projectID)
	if accessErr != nil || !access.CanRead {
		// Not found rather than forbidden: someone who cannot see a project
		// should not learn it exists.
		RespondError(w, utils.NewError(nil, domain.ERR_PROJECT_NOT_FOUND_ERR_CODE,
			errors.New(domain.ERR_PROJECT_NOT_FOUND_ERR)))
		return
	}
	if !access.CanDelete {
		RespondError(w, utils.NewError(nil, domain.UNAUTHORIZED,
			errors.New("You cannot delete this project")))
		return
	}

	_, err = h.svc.DeleteProject(ctx, projectID)
	if err != nil {
		log.WithError(err).Error("Failed to delete project")
		RespondError(w, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (h *ProjectHandler) Validate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	log := cslog.L(ctx)

	project, err := middleware.GetProjectFromContext(ctx)
	if err != nil {
		log.WithError(err).Error("Failed to get project from context")
		RespondError(w, err)
		return
	}

	// The SDK calls this once per launch, which makes it the one channel the
	// server already has to the client. Sampling rides along rather than
	// needing a second endpoint and a second round trip at startup.
	RespondJSON(w, http.StatusOK, map[string]any{
		"id":                  project.ID,
		"name":                project.Name,
		"session_sample_rate": project.SampleFraction(),
	})
}
