package db

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/pkg/cslog"
)

// A personal access token outlives any session: it sits in editor configs and
// shell histories for months, and it authenticates the MCP endpoint on every
// request. The same discipline as the session token applies, with one more
// clause: the HASH stays out of the log too. A line carrying the hash says
// which token was used where, which is more than any reader needs.
//
// Asserts on the bytes the logger actually produced, at debug, so a WithField
// added later still trips it.
func TestThePersonalTokenIsNeverLogged(t *testing.T) {
	db := testDB(t)
	repo := NewTokenRepo(db)

	logger := cslog.GetLogger()
	oldOut, oldLevel := logger.Out, logger.Level
	t.Cleanup(func() { logger.SetOutput(oldOut); logger.SetLevel(oldLevel) })

	var out bytes.Buffer
	logger.SetOutput(&out)
	logger.SetLevel(logrus.DebugLevel)
	ctx := cslog.WithLogger(context.Background(), logrus.NewEntry(logger))

	user := &domain.User{
		ID: uuid.New(), Email: "token-logs-" + uuid.NewString() + "@example.com",
		Name: "Token Logs", PasswordHash: "x", Role: domain.RoleMember,
	}
	if err := NewUserRepo(db).Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Where("id = ?", user.ID).Delete(&UserModel{}) })

	plaintext, hash, err := domain.MintPersonalToken()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	token := &domain.PersonalAccessToken{
		UserID: user.ID, Name: "logged?", TokenHash: hash,
		Suffix: domain.TokenSuffix(plaintext),
	}
	if err := repo.Create(ctx, token); err != nil {
		t.Fatalf("create token: %v", err)
	}
	t.Cleanup(func() {
		db.Unscoped().Where("user_id = ?", user.ID).Delete(&PersonalAccessTokenModel{})
	})

	// Every path that takes the credential or its hash: the hit, the miss,
	// the revoke, and the cleanup sweep.
	if _, err := repo.GetByHash(ctx, hash); err != nil {
		t.Fatalf("get by hash: %v", err)
	}
	if _, err := repo.GetByHash(ctx, domain.HashPersonalToken("csp_not-a-real-token")); err == nil {
		t.Fatal("expected a miss for an unknown hash")
	}
	if err := repo.Revoke(ctx, user.ID, token.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := repo.DeleteByUser(ctx, user.ID); err != nil {
		t.Fatalf("delete by user: %v", err)
	}

	if out.Len() == 0 {
		t.Fatal("nothing was logged at all, so this test proves nothing")
	}
	logged := out.String()
	if strings.Contains(logged, plaintext) {
		t.Errorf("the token plaintext reached the log:\n%s", logged)
	}
	if strings.Contains(logged, hash) {
		t.Errorf("the token hash reached the log:\n%s", logged)
	}
	// A miss is a revoked token still configured somewhere — ordinary
	// traffic, not a fault.
	if strings.Contains(logged, "level=error") {
		t.Errorf("a token miss was logged as an error:\n%s", logged)
	}
}
