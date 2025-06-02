package middleware

import (
	"context"
	"errors"
	"fmt"

	"net/http"
	"regexp"

	"github.com/google/uuid"
	error_codes "github.com/t0uh33d/code_scout/models/codes"
	"github.com/t0uh33d/code_scout/models/db"
	"github.com/t0uh33d/code_scout/utils"
)

type contextKey string

const projectContextKey = contextKey("project")

const (
	apiProjectIDHeaderKey     = "X-Project-ID"
	apiProjectSecretHeaderKey = "X-Project-Secret"
)

var Authenticate = func(next http.Handler) http.Handler {

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

			uuid, err := uuid.Parse(id)
			if err != nil {
				err := utils.NewError(nil, error_codes.INVALID_PROJECT_ID_HEADER_ERR_CODE, errors.New(error_codes.INVALID_PROJECT_ID_HEADER_ERR))
				utils.HttpError(w, http.StatusBadRequest, err)
				return
			}

			project, err := db.GetProjectByID(db.GormDB, uuid)
			if err != nil {
				err := utils.NewError(nil, error_codes.INVALID_PROJECT_ID_HEADER_ERR_CODE, errors.New(error_codes.INVALID_PROJECT_ID_HEADER_ERR))
				utils.HttpError(w, http.StatusBadRequest, err)
				return
			}

			dbProjectSecret, err := db.GetProjectSecretByProjectID(db.GormDB, project.ID)
			if err != nil {
				err := utils.NewError(nil, error_codes.INVALID_PROJECT_ID_HEADER_ERR_CODE, errors.New(error_codes.INVALID_PROJECT_ID_HEADER_ERR))
				utils.HttpError(w, http.StatusBadRequest, err)
				return
			}

			if dbProjectSecret == nil || dbProjectSecret.SecretKey != sec {
				err := utils.NewError(nil, error_codes.INVALID_PROJECT_SECRET_HEADER_ERR_CODE, errors.New(error_codes.INVALID_PROJECT_SECRET_HEADER_ERR))
				utils.HttpError(w, http.StatusBadRequest, err)
				return
			}

			ctx := context.WithValue(r.Context(), projectContextKey, project)

			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// authHeader, err := getAuthorizationToken(r)
		// if err != nil {
		// 	utils.HttpError(w, http.StatusBadRequest, err)
		// 	return
		// }
		// // Check if the token is in the "Bearer <token>" format
		// tokenParts := strings.Split(authHeader, " ")
		// if len(tokenParts) != 2 || tokenParts[0] != "Bearer" {
		// 	err := utils.NewError(nil, error_codes.INVALID_AUTHORIZATION_TOKEN_FORMAT, errors.New(error_codes.INVALID_AUTHORIZATION_TOKEN_FORMAT_ERR))
		// 	utils.HttpError(w, http.StatusBadRequest, err)
		// 	return
		// }
		// token := tokenParts[1]

		// // Create a new context with the populated user
		// ctx := context.WithValue(r.Context(), userContextKey, populatedUser)

		// Call the next handler with the new context
		next.ServeHTTP(w, r)
	})
}

func getAuthorizationToken(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", utils.NewError(nil, error_codes.AUTHORIZATION_HEADER_MISSING, errors.New(error_codes.AUTHORIZATION_HEADER_MISSING_ERR))
	}
	return authHeader, nil
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
		return "", "", utils.NewError(nil, error_codes.PROJECT_ID_HEADER_MISSING_ERR_CODE, errors.New(error_codes.PROJECT_ID_HEADER_MISSING_ERR))
	}

	projectSecret := r.Header.Get(apiProjectSecretHeaderKey)
	if projectSecret == "" {
		return "", "", utils.NewError(nil, error_codes.PROJECT_SECRET_HEADER_MISSING_ERR_CODE, errors.New(error_codes.PROJECT_SECRET_HEADER_MISSING_ERR))
	}

	return projectID, projectSecret, nil
}

func GetProjectFromContext(ctx context.Context) (*db.Projects, error) {
	project, ok := ctx.Value(projectContextKey).(*db.Projects)
	if !ok || project == nil {
		return nil, utils.NewError(nil, error_codes.INVALID_PROJECT_ID_HEADER_ERR_CODE, errors.New(error_codes.INVALID_PROJECT_ID_HEADER_ERR))
	}
	return project, nil
}
