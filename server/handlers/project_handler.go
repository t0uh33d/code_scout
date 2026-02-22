package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/internal/ports"
	"github.com/t0uh33d/code_scout/pkg/cslog"
	"github.com/t0uh33d/code_scout/pkg/utils"
	"github.com/t0uh33d/code_scout/server/middleware"
)

type ProjectHandler struct {
	svc ports.ProjectManager
}

func NewProjectHandler(svc ports.ProjectManager) *ProjectHandler {
	return &ProjectHandler{svc: svc}
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

	RespondJSON(w, http.StatusOK, project)
}
