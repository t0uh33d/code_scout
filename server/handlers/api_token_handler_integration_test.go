package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	dbadapter "github.com/getcodescout/code_scout/internal/adapters/db"
	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/services"
	"github.com/getcodescout/code_scout/server/middleware"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// The claim under test spans the handler, the service and the row (the
// plaintext appears in exactly one response and nowhere at rest), so it runs
// against a real Postgres. Skips without CS_TEST_DB, same as the rest.
func tokenHandlerDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("CS_TEST_DB")
	if dsn == "" {
		t.Skip("CS_TEST_DB not set, skipping integration test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	// A small, closed pool, so parallel packages cannot exhaust Postgres.
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(4)
		t.Cleanup(func() { sqlDB.Close() })
	}
	if err := dbadapter.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func tokenHandlerUser(t *testing.T, db *gorm.DB) *domain.User {
	t.Helper()
	repo := dbadapter.NewUserRepo(db)
	user := &domain.User{
		Name: "Pane Test", Email: "pane-" + uuid.NewString() + "@example.com",
		PasswordHash: "x", Role: domain.RoleMember,
	}
	if err := repo.Create(context.Background(), user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		db.Unscoped().Where("user_id = ?", user.ID).Delete(&dbadapter.PersonalAccessTokenModel{})
		db.Unscoped().Where("id = ?", user.ID).Delete(&dbadapter.UserModel{})
	})
	return user
}

// plaintextPattern matches a whole minted token: the prefix plus 43 base64url
// characters. Matching the shape rather than one known value is what makes the
// "absent later" half of the test meaningful.
var plaintextPattern = regexp.MustCompile(`csp_[A-Za-z0-9_-]{43}`)

func postCreate(t *testing.T, h *APITokenHandler, user *domain.User, name string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"name": {name}, "expires": {"90d"}}
	r := httptest.NewRequest(http.MethodPost, "/settings/tokens", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r = r.WithContext(middleware.WithUser(r.Context(), user))
	rec := httptest.NewRecorder()
	h.CreateToken(rec, r)
	return rec
}

// postRevoke drives RevokeToken; with an unknown id it is also the cheapest
// way to make the handler re-render the pane from the database.
func postRevoke(t *testing.T, h *APITokenHandler, user *domain.User, id string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/settings/tokens/"+id+"/revoke", nil)
	r = mux.SetURLVars(r, map[string]string{"id": id})
	r = r.WithContext(middleware.WithUser(r.Context(), user))
	rec := httptest.NewRecorder()
	h.RevokeToken(rec, r)
	return rec
}

func TestTheCreateResponseShowsThePlaintextExactlyOnce(t *testing.T) {
	db := tokenHandlerDB(t)
	user := tokenHandlerUser(t, db)
	h := NewAPITokenHandler(services.NewTokenService(dbadapter.NewTokenRepo(db), dbadapter.NewUserRepo(db)))

	created := postCreate(t, h, user, "editor")
	if created.Code != http.StatusOK {
		t.Fatalf("create status = %d", created.Code)
	}
	plaintext := plaintextPattern.FindString(created.Body.String())
	if plaintext == "" {
		t.Fatal("the create response does not show the token")
	}

	// Any later response, rendered from what is stored: the shape of a full
	// token must be gone, while the display suffix survives.
	later := postRevoke(t, h, user, uuid.NewString())
	if got := plaintextPattern.FindString(later.Body.String()); got != "" {
		t.Errorf("a later response still carries a full token: %s", got)
	}
	suffix := plaintext[len(plaintext)-4:]
	if !strings.Contains(later.Body.String(), "csp_…"+suffix) {
		t.Error("the later response does not show the token's display suffix")
	}
}

func TestYouOnlySeeYourOwnTokens(t *testing.T) {
	db := tokenHandlerDB(t)
	mine := tokenHandlerUser(t, db)
	theirs := tokenHandlerUser(t, db)
	svc := services.NewTokenService(dbadapter.NewTokenRepo(db), dbadapter.NewUserRepo(db))
	h := NewAPITokenHandler(svc)

	if _, _, err := svc.Create(context.Background(), mine.ID, "my-editor", nil); err != nil {
		t.Fatalf("create mine: %v", err)
	}
	if _, _, err := svc.Create(context.Background(), theirs.ID, "their-editor", nil); err != nil {
		t.Fatalf("create theirs: %v", err)
	}

	pane := postRevoke(t, h, mine, uuid.NewString()).Body.String()
	if !strings.Contains(pane, "my-editor") {
		t.Error("my own token is missing from my pane")
	}
	if strings.Contains(pane, "their-editor") {
		t.Error("somebody else's token is on my pane")
	}
}
