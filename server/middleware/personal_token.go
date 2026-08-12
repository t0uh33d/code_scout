package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/pkg/utils"
)

// TokenAuthenticator is the narrow slice of TokenService this middleware
// needs, defined here so tests can hand in a fake.
type TokenAuthenticator interface {
	Authenticate(ctx context.Context, plaintext string) (*domain.User, error)
}

// RequirePersonalToken authenticates `Authorization: Bearer csp_…` and puts
// the resolved user on the context under the same key RequireSession uses, so
// UserFrom works identically behind either. MCP clients see these errors as
// plain HTTP before any protocol handshake, which is intended: a bad token
// should fail loudly at setup, not inside a tool call.
//
// The bearer value is handed straight to Authenticate, which hashes it in its
// first statement. It is never logged and never stored.
func RequirePersonalToken(tokenSvc TokenAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			refuse := func(err error) {
				w.Header().Set("WWW-Authenticate", "Bearer")
				w.Header().Set("Content-Type", "application/json")
				WriteError(w, err)
			}

			header := r.Header.Get("Authorization")
			scheme, token, found := strings.Cut(header, " ")
			if header == "" || !found || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
				refuse(utils.NewError(nil, domain.ERR_TOKEN_MISSING_ERR_CODE, errors.New(domain.ERR_TOKEN_MISSING_ERR)))
				return
			}

			user, err := tokenSvc.Authenticate(r.Context(), strings.TrimSpace(token))
			if err != nil {
				refuse(err)
				return
			}

			next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
		})
	}
}

// WithUser puts an authenticated user on a context, the same way
// RequireSession does. Exported so tests can exercise handlers and tools
// without standing up either middleware.
func WithUser(ctx context.Context, user *domain.User) context.Context {
	return context.WithValue(ctx, userCtxKey, user)
}
