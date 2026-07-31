package services_test

import (
	"context"
	"os"
	"testing"

	dbadapter "github.com/t0uh33d/code_scout/internal/adapters/db"
	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/internal/services"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// ResetPassword crosses the service and the repo (hash update plus session
// invalidation must both happen), so it is tested against a real Postgres.
// Skips without CS_TEST_DB, same convention as the repo integration tests.
func authTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("CS_TEST_DB")
	if dsn == "" {
		t.Skip("CS_TEST_DB not set, skipping integration test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := dbadapter.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedUser(t *testing.T, db *gorm.DB, email, password string) *domain.User {
	t.Helper()
	repo := dbadapter.NewUserRepo(db)
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	user := &domain.User{Name: "Reset Test", Email: email, PasswordHash: string(hash)}
	if err := repo.Create(context.Background(), user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		db.Unscoped().Where("user_id = ?", user.ID).Delete(&dbadapter.UserSessionModel{})
		db.Unscoped().Where("email = ?", email).Delete(&dbadapter.UserModel{})
	})
	return user
}

func TestResetPasswordRoundTrip(t *testing.T) {
	db := authTestDB(t)
	repo := dbadapter.NewUserRepo(db)
	svc := services.NewAuthService(repo)
	ctx := context.Background()

	user := seedUser(t, db, "reset-roundtrip@test.local", "original-pass")

	// Two live sessions that must both die with the reset.
	for range 2 {
		token, _, err := loginFor(svc, ctx, user.Email, "original-pass")
		if err != nil {
			t.Fatalf("pre-reset login: %v", err)
		}
		if token == "" {
			t.Fatal("expected a session token")
		}
	}

	// Case-insensitive lookup is part of the contract: the operator types the
	// email by hand on a server console.
	temp, err := svc.ResetPassword(ctx, "  RESET-ROUNDTRIP@test.local ")
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if len(temp) != 16 {
		t.Fatalf("temp password length = %d, want 16", len(temp))
	}

	// Old password is dead, temp password works.
	if _, _, err := loginFor(svc, ctx, user.Email, "original-pass"); err == nil {
		t.Fatal("old password still logs in after reset")
	}
	if _, _, err := loginFor(svc, ctx, user.Email, temp); err != nil {
		t.Fatalf("temp password rejected: %v", err)
	}

	// Every pre-reset session is gone; only the post-reset login remains.
	var count int64
	db.Model(&dbadapter.UserSessionModel{}).Where("user_id = ?", user.ID).Count(&count)
	if count != 1 {
		t.Fatalf("sessions after reset+login = %d, want 1", count)
	}
}

func TestResetPasswordUnknownEmail(t *testing.T) {
	db := authTestDB(t)
	svc := services.NewAuthService(dbadapter.NewUserRepo(db))

	if _, err := svc.ResetPassword(context.Background(), "ghost@test.local"); err == nil {
		t.Fatal("expected an error for an unknown email")
	}
}

// loginFor drives the real login path so the assertions cover what a user
// would actually experience, not just the stored hash.
func loginFor(svc *services.AuthService, ctx context.Context, email, password string) (string, bool, error) {
	token, isNew, _, err := svc.LoginOrRegister(ctx, &domain.AuthOpts{Email: email, Password: password})
	return token, isNew, err
}
