package services_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	dbadapter "github.com/getcodescout/code_scout/internal/adapters/db"
	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/services"
	"github.com/getcodescout/code_scout/pkg/utils"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// The behaviour under test spans the service and the row it stores (hash-only
// storage, the unique index, soft-delete visibility), so it runs against a
// real Postgres. Skips without CS_TEST_DB, same convention as the rest.
func tokenTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("CS_TEST_DB")
	if dsn == "" {
		t.Skip("CS_TEST_DB not set, skipping integration test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Bound the pool and close it with the test.
	//
	// `go test ./...` runs packages concurrently, this helper is called once per
	// test, and an unbounded pool that nothing closes means a full run opens
	// connections until Postgres refuses: "sorry, too many clients already".
	// That turned CI red at random, on commits that changed no Go code at all,
	// which is worse than a test that fails honestly — a suite that cries wolf
	// teaches everyone to merge through red.
	//
	// One test needs one connection. Two, closed on cleanup, bounds the whole
	// suite at a couple of dozen however many packages run at once.
	pool, err := db.DB()
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	pool.SetMaxOpenConns(2)
	pool.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	// A small, closed pool: the suite opens one of these per test, and a
	// hundred forgotten pools is how a local Postgres runs out of slots.
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(4)
		t.Cleanup(func() { sqlDB.Close() })
	}
	if err := dbadapter.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func tokenTestUser(t *testing.T, db *gorm.DB, mustChange bool) *domain.User {
	t.Helper()
	repo := dbadapter.NewUserRepo(db)
	user := &domain.User{
		Name: "Token Test", Email: "tokens-" + uuid.NewString() + "@example.com",
		PasswordHash: "x", Role: domain.RoleMember, MustChangePassword: mustChange,
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

func tokenService(db *gorm.DB) *services.TokenService {
	return services.NewTokenService(dbadapter.NewTokenRepo(db), dbadapter.NewUserRepo(db))
}

func errCode(t *testing.T, err error) int {
	t.Helper()
	var ej *utils.ErrorJson
	if !errors.As(err, &ej) {
		t.Fatalf("error is not an ErrorJson: %v", err)
	}
	return ej.ErrorCode
}

// The reason this table exists at all: the row must be worthless to read.
func TestCreateStoresOnlyTheHash(t *testing.T) {
	db := tokenTestDB(t)
	user := tokenTestUser(t, db, false)
	svc := tokenService(db)
	ctx := context.Background()

	plaintext, token, err := svc.Create(ctx, user.ID, "editor", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	var row dbadapter.PersonalAccessTokenModel
	if err := db.Where("id = ?", token.ID).First(&row).Error; err != nil {
		t.Fatalf("read row back: %v", err)
	}
	if row.TokenHash != domain.HashPersonalToken(plaintext) {
		t.Error("stored hash is not the hash of the returned plaintext")
	}
	if row.TokenHash == plaintext {
		t.Error("the plaintext was stored as the hash")
	}
	// The row's string columns, together. The suffix legitimately carries the
	// last four characters; nothing may carry more.
	if len(plaintext) < 8 {
		t.Fatal("token too short for this check to mean anything")
	}
	secretBody := plaintext[:len(plaintext)-4]
	for _, col := range []string{row.Name, row.TokenHash, row.Suffix} {
		if strings.Contains(col, secretBody) {
			t.Errorf("a stored column carries the secret: %q", col)
		}
	}
	if row.Suffix != plaintext[len(plaintext)-4:] {
		t.Errorf("suffix = %q, want the plaintext's last four characters", row.Suffix)
	}
}

func TestAuthenticateAcceptsAMintedToken(t *testing.T) {
	db := tokenTestDB(t)
	user := tokenTestUser(t, db, false)
	svc := tokenService(db)
	ctx := context.Background()

	plaintext, _, err := svc.Create(ctx, user.ID, "editor", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := svc.Authenticate(ctx, plaintext)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if got.ID != user.ID {
		t.Errorf("authenticated as %s, want %s", got.ID, user.ID)
	}
}

func TestAuthenticateRejectsAnUnknownToken(t *testing.T) {
	db := tokenTestDB(t)
	svc := tokenService(db)

	// A perfectly-formed token that was never created.
	unknown, _, err := domain.MintPersonalToken()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	_, err = svc.Authenticate(context.Background(), unknown)
	if err == nil {
		t.Fatal("an unknown token authenticated")
	}
	if errCode(t, err) != domain.ERR_TOKEN_INVALID_ERR_CODE {
		t.Errorf("code = %d, want invalid", errCode(t, err))
	}
}

func TestAuthenticateRejectsAnExpiredToken(t *testing.T) {
	db := tokenTestDB(t)
	user := tokenTestUser(t, db, false)
	svc := tokenService(db)
	ctx := context.Background()

	expired := -time.Minute
	plaintext, _, err := svc.Create(ctx, user.ID, "old", &expired)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = svc.Authenticate(ctx, plaintext)
	if err == nil {
		t.Fatal("an expired token authenticated")
	}
	if errCode(t, err) != domain.ERR_TOKEN_EXPIRED_ERR_CODE {
		t.Errorf("code = %d, want expired", errCode(t, err))
	}
}

func TestAuthenticateRejectsARevokedToken(t *testing.T) {
	db := tokenTestDB(t)
	user := tokenTestUser(t, db, false)
	svc := tokenService(db)
	ctx := context.Background()

	plaintext, token, err := svc.Create(ctx, user.ID, "doomed", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.Revoke(ctx, user.ID, token.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	_, err = svc.Authenticate(ctx, plaintext)
	if err == nil {
		t.Fatal("a revoked token authenticated")
	}
	// Same code as unknown on purpose: a revoked token's holder learns
	// nothing about what happened to it.
	if errCode(t, err) != domain.ERR_TOKEN_INVALID_ERR_CODE {
		t.Errorf("code = %d, want invalid", errCode(t, err))
	}
}

func TestRevokeIsScopedToTheOwner(t *testing.T) {
	db := tokenTestDB(t)
	owner := tokenTestUser(t, db, false)
	other := tokenTestUser(t, db, false)
	svc := tokenService(db)
	ctx := context.Background()

	plaintext, token, err := svc.Create(ctx, owner.ID, "mine", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := svc.Revoke(ctx, other.ID, token.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("someone else's revoke answered %v, want not found", err)
	}
	if _, err := svc.Authenticate(ctx, plaintext); err != nil {
		t.Error("the token stopped working after a revoke that should have done nothing")
	}
}

func TestAuthenticateRefusesAnAccountOnATemporaryPassword(t *testing.T) {
	db := tokenTestDB(t)
	user := tokenTestUser(t, db, true)
	svc := tokenService(db)
	ctx := context.Background()

	plaintext, _, err := svc.Create(ctx, user.ID, "blocked", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = svc.Authenticate(ctx, plaintext)
	if err == nil {
		t.Fatal("an account on a temporary password authenticated over a token")
	}
	if errCode(t, err) != domain.ERR_TOKEN_PASSWORD_CHANGE_ERR_CODE {
		t.Errorf("code = %d, want password-change", errCode(t, err))
	}
}

func TestLastUsedAtIsThrottled(t *testing.T) {
	db := tokenTestDB(t)
	user := tokenTestUser(t, db, false)
	svc := tokenService(db)
	ctx := context.Background()

	plaintext, token, err := svc.Create(ctx, user.ID, "busy", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	readStamp := func() *time.Time {
		var row dbadapter.PersonalAccessTokenModel
		if err := db.Where("id = ?", token.ID).First(&row).Error; err != nil {
			t.Fatalf("read row: %v", err)
		}
		return row.LastUsedAt
	}

	if _, err := svc.Authenticate(ctx, plaintext); err != nil {
		t.Fatalf("first authenticate: %v", err)
	}
	first := readStamp()
	if first == nil {
		t.Fatal("the first authentication left no last_used_at")
	}

	if _, err := svc.Authenticate(ctx, plaintext); err != nil {
		t.Fatalf("second authenticate: %v", err)
	}
	second := readStamp()
	if second == nil || !second.Equal(*first) {
		t.Errorf("a second authentication seconds later rewrote last_used_at: %v -> %v", first, second)
	}
}

func TestTheTokenLimitIsEnforced(t *testing.T) {
	db := tokenTestDB(t)
	user := tokenTestUser(t, db, false)
	svc := tokenService(db)
	ctx := context.Background()

	for i := range 20 {
		if _, _, err := svc.Create(ctx, user.ID, "bulk", nil); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	_, _, err := svc.Create(ctx, user.ID, "one too many", nil)
	if err == nil {
		t.Fatal("the 21st token was created")
	}
	if errCode(t, err) != domain.ERR_TOKEN_LIMIT_ERR_CODE {
		t.Errorf("code = %d, want limit", errCode(t, err))
	}
}
