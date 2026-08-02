package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/t0uh33d/code_scout/internal/domain"
)

// AccessResolver is what RequireProjectAccess needs from the member service.
type AccessResolver interface {
	ResolveAccess(ctx context.Context, actor *domain.User, projectID uuid.UUID) (domain.ProjectAccess, error)
}

const projectAccessCtxKey ctxKey = "cs_project_access"
const projectIDCtxKey ctxKey = "cs_project_id"

// ProjectAccessFrom returns what the signed-in user may do in the project this
// request is about. Zero value means no access.
func ProjectAccessFrom(ctx context.Context) domain.ProjectAccess {
	access, _ := ctx.Value(projectAccessCtxKey).(domain.ProjectAccess)
	return access
}

// ProjectIDFrom returns the already-parsed project id.
func ProjectIDFrom(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(projectIDCtxKey).(uuid.UUID)
	return id
}

// RequireProjectAccess gates every route under /project/{id}. Making this
// middleware rather than a per-handler check is deliberate: a handler that
// forgets the check would silently expose another team's logs, and there is no
// test that catches a check nobody wrote.
//
// A project the caller cannot see returns 404, never 403. Someone who is not on
// a project should not be able to learn that it exists.
func RequireProjectAccess(resolver AccessResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			projectID, err := uuid.Parse(mux.Vars(r)["id"])
			if err != nil {
				http.NotFound(w, r)
				return
			}

			access, err := resolver.ResolveAccess(r.Context(), UserFrom(r.Context()), projectID)
			if err != nil || !access.CanRead {
				http.NotFound(w, r)
				return
			}

			ctx := context.WithValue(r.Context(), projectAccessCtxKey, access)
			ctx = context.WithValue(ctx, projectIDCtxKey, projectID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireProjectAccessFromQuery is the same gate for routes that carry the
// project id as a query parameter rather than a path variable.
//
// These are easy to miss precisely because they do not look project-scoped:
// /export/logs and /stream/logs read like utility endpoints, but both take a
// project id and hand back that project's contents. Without this, anyone signed
// in could export or stream any project by guessing its id.
func RequireProjectAccessFromQuery(resolver AccessResolver, param string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			projectID, err := uuid.Parse(r.URL.Query().Get(param))
			if err != nil {
				http.NotFound(w, r)
				return
			}

			access, err := resolver.ResolveAccess(r.Context(), UserFrom(r.Context()), projectID)
			if err != nil || !access.CanRead {
				http.NotFound(w, r)
				return
			}

			ctx := context.WithValue(r.Context(), projectAccessCtxKey, access)
			ctx = context.WithValue(ctx, projectIDCtxKey, projectID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireProjectManage additionally demands the rights to change a project:
// its settings, its secret, and who is on it.
func RequireProjectManage(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ProjectAccessFrom(r.Context()).CanManage {
			http.Error(w, "You cannot change this project", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireProjectDelete demands the rights to destroy a project, which needs
// maintainer on it AND the Admin role.
func RequireProjectDelete(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ProjectAccessFrom(r.Context()).CanDelete {
			http.Error(w, "You cannot delete this project", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
