package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/pkg/utils"
)

type stubAuthenticator struct {
	user *domain.User
	err  error
	// got is the bearer value the middleware handed over, so tests can assert
	// the header was parsed rather than passed through raw.
	got string
}

func (s *stubAuthenticator) Authenticate(_ context.Context, plaintext string) (*domain.User, error) {
	s.got = plaintext
	if s.err != nil {
		return nil, s.err
	}
	return s.user, nil
}

func runPersonalToken(t *testing.T, auth *stubAuthenticator, header string) (*httptest.ResponseRecorder, *domain.User) {
	t.Helper()
	var seen *domain.User
	handler := RequirePersonalToken(auth)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = UserFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/mcp", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec, seen
}

func decodeErrorBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not JSON: %v\n%s", err, rec.Body.String())
	}
	return body
}

func TestMissingBearerAnswers401JSON(t *testing.T) {
	for name, header := range map[string]string{
		"no header":    "",
		"wrong scheme": "Basic dXNlcjpwYXNz",
		"bare scheme":  "Bearer",
		"empty token":  "Bearer   ",
	} {
		t.Run(name, func(t *testing.T) {
			auth := &stubAuthenticator{user: &domain.User{}}
			rec, seen := runPersonalToken(t, auth, header)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
			if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
				t.Errorf("WWW-Authenticate = %q, want Bearer", got)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
			body := decodeErrorBody(t, rec)
			if code, _ := body["code"].(float64); int(code) != domain.ERR_TOKEN_MISSING_ERR_CODE {
				t.Errorf("code = %v, want %d", body["code"], domain.ERR_TOKEN_MISSING_ERR_CODE)
			}
			if seen != nil {
				t.Error("the handler ran without a token")
			}
			if auth.got != "" {
				t.Errorf("the service was called with %q; a missing token must be refused before the service", auth.got)
			}
		})
	}
}

func TestAMalformedBearerAnswers401JSON(t *testing.T) {
	auth := &stubAuthenticator{
		err: utils.NewError(nil, domain.ERR_TOKEN_INVALID_ERR_CODE, errors.New(domain.ERR_TOKEN_INVALID_ERR)),
	}
	rec, seen := runPersonalToken(t, auth, "Bearer csp_notatoken")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	body := decodeErrorBody(t, rec)
	if code, _ := body["code"].(float64); int(code) != domain.ERR_TOKEN_INVALID_ERR_CODE {
		t.Errorf("code = %v, want %d", body["code"], domain.ERR_TOKEN_INVALID_ERR_CODE)
	}
	if seen != nil {
		t.Error("the handler ran with an invalid token")
	}
	if auth.got != "csp_notatoken" {
		t.Errorf("service saw %q, want the bare token without the scheme", auth.got)
	}
}

func TestAMustChangePasswordRefusalIs403(t *testing.T) {
	auth := &stubAuthenticator{
		err: utils.NewError(nil, domain.ERR_TOKEN_PASSWORD_CHANGE_ERR_CODE, errors.New(domain.ERR_TOKEN_PASSWORD_CHANGE_ERR)),
	}
	rec, _ := runPersonalToken(t, auth, "Bearer csp_x")
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestAValidTokenPutsTheUserOnTheContext(t *testing.T) {
	user := &domain.User{Name: "Robo", Role: domain.RoleMember}
	auth := &stubAuthenticator{user: user}
	rec, seen := runPersonalToken(t, auth, "Bearer csp_good")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if seen != user {
		t.Error("UserFrom did not return the authenticated user inside the handler")
	}
}

// A lower-cased scheme is valid per RFC 7235; clients disagree on the casing.
func TestTheBearerSchemeIsCaseInsensitive(t *testing.T) {
	auth := &stubAuthenticator{user: &domain.User{}}
	rec, seen := runPersonalToken(t, auth, "bearer csp_good")
	if rec.Code != http.StatusOK || seen == nil {
		t.Errorf("lower-case scheme refused: status %d", rec.Code)
	}
}
