package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/internal/ports"
	"github.com/t0uh33d/code_scout/pkg/utils"
)

type contextKey string

const projectContextKey = contextKey("project")

const (
	apiProjectIDHeaderKey     = "X-Project-ID"
	apiProjectSecretHeaderKey = "X-Project-Secret"
)

func Authenticate(projectSvc ports.ProjectManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			requestPath := r.URL.Path
			requestMethod := r.Method
			fmt.Println("the request path :", requestMethod, requestPath)
			notAuthEndpoints := map[string]string{
				"/api/login": "POST",
			}

			if yes := isRouteParOf(notAuthEndpoints, requestPath, requestMethod); yes {
				next.ServeHTTP(w, r)
				return
			}

			mobCommEndpoints := map[string]string{
				"/api/logs/dump": "POST",
				"/api/validate":  "GET",
			}

			if yes := isRouteParOf(mobCommEndpoints, requestPath, requestMethod); yes {
				id, sec, err := getProjectIDAndSecret(r)
				if err != nil {
					utils.HttpError(w, http.StatusBadRequest, err)
					return
				}

				projectUUID, err := uuid.Parse(id)
				if err != nil {
					err := utils.NewError(nil, domain.INVALID_PROJECT_ID_HEADER_ERR_CODE, errors.New(domain.INVALID_PROJECT_ID_HEADER_ERR))
					utils.HttpError(w, http.StatusBadRequest, err)
					return
				}

				project, status, err := projectSvc.ValidateProjectCredentials(ctx, projectUUID, sec)
				if err != nil {
					utils.HttpError(w, status, err)
					return
				}

				ctx := context.WithValue(r.Context(), projectContextKey, project)

				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isRouteParOf(mp map[string]string, path string, method string) bool {
	for p, m := range mp {
		validPath, _ := regexp.MatchString(p, path)
		if validPath && m == method {
			return true
		}
	}
	return false
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

func GetProjectFromContext(ctx context.Context) (*domain.Project, error) {
	project, ok := ctx.Value(projectContextKey).(*domain.Project)
	if !ok || project == nil {
		return nil, utils.NewError(nil, domain.INVALID_PROJECT_ID_HEADER_ERR_CODE, errors.New(domain.INVALID_PROJECT_ID_HEADER_ERR))
	}
	return project, nil
}
