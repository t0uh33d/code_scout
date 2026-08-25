package middleware

import (
	"context"
	"errors"
	"net/http"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/ports"
	"github.com/getcodescout/code_scout/pkg/utils"
	"github.com/google/uuid"
)

type contextKey string

const projectContextKey = contextKey("project")

const (
	apiProjectIDHeaderKey     = "X-Project-ID"
	apiProjectSecretHeaderKey = "X-Project-Secret"
)

// Authenticate validates SDK credentials (X-Project-ID / X-Project-Secret) on
// every route it wraps and puts the resolved project on the request context.
// There is deliberately no allowlist: anything mounted behind this middleware
// requires valid credentials.
func Authenticate(projectSvc ports.ProjectManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, sec, err := getProjectIDAndSecret(r)
			if err != nil {
				utils.HttpError(w, http.StatusUnauthorized, err)
				return
			}

			projectUUID, err := uuid.Parse(id)
			if err != nil {
				err := utils.NewError(nil, domain.INVALID_PROJECT_ID_HEADER_ERR_CODE, errors.New(domain.INVALID_PROJECT_ID_HEADER_ERR))
				utils.HttpError(w, http.StatusBadRequest, err)
				return
			}

			project, status, err := projectSvc.ValidateProjectCredentials(r.Context(), projectUUID, sec)
			if err != nil {
				utils.HttpError(w, status, err)
				return
			}

			next.ServeHTTP(w, r.WithContext(WithProject(r.Context(), project)))
		})
	}
}

func getProjectIDAndSecret(r *http.Request) (string, string, error) {
	projectID := r.Header.Get(apiProjectIDHeaderKey)
	if projectID == "" {
		return "", "", utils.NewError(nil, domain.PROJECT_ID_HEADER_MISSING_ERR_CODE, errors.New(domain.PROJECT_ID_HEADER_MISSING_ERR))
	}

	projectSecret := r.Header.Get(apiProjectSecretHeaderKey)
	if projectSecret == "" {
		return "", "", utils.NewError(nil, domain.PROJECT_SECRET_HEADER_MISSING_ERR_CODE, errors.New(domain.PROJECT_SECRET_HEADER_MISSING_ERR))
	}

	return projectID, projectSecret, nil
}

// WithProject puts an authenticated project on a context. Authenticate uses it,
// and so does anything that needs to exercise a handler without standing up the
// credential middleware and a database behind it.
func WithProject(ctx context.Context, project *domain.Project) context.Context {
	return context.WithValue(ctx, projectContextKey, project)
}

func GetProjectFromContext(ctx context.Context) (*domain.Project, error) {
	project, ok := ctx.Value(projectContextKey).(*domain.Project)
	if !ok || project == nil {
		return nil, utils.NewError(nil, domain.INVALID_PROJECT_ID_HEADER_ERR_CODE, errors.New(domain.INVALID_PROJECT_ID_HEADER_ERR))
	}
	return project, nil
}
