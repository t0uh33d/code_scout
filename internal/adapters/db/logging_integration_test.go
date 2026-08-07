package db

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/pkg/cslog"
)

// The session token is the whole of the cs_session cookie. A log line carrying
// one is a line anyone who can read the log can paste into a browser and become
// that user, and it was being written at debug on every single request that
// arrived with a cookie — 626 lines and 6 distinct live tokens in the dev
// server's journal by the time anyone looked.
//
// This asserts on the bytes the logger actually produced rather than on the
// call sites, so it still holds when somebody adds a WithField later, and it
// runs at debug so nothing is hidden by the level.
func TestTheSessionTokenIsNeverLogged(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepo(db)

	logger := cslog.GetLogger()
	oldOut, oldLevel := logger.Out, logger.Level
	t.Cleanup(func() { logger.SetOutput(oldOut); logger.SetLevel(oldLevel) })

	var out bytes.Buffer
	logger.SetOutput(&out)
	logger.SetLevel(logrus.DebugLevel)
	ctx := cslog.WithLogger(context.Background(), logrus.NewEntry(logger))

	// The column is a uuid, so the token has to be one. Freshly generated, so it
	// cannot collide with anything else in the output.
	token := uuid.New().String()

	user := &domain.User{
		ID: uuid.New(), Email: "logs@example.com", Name: "Logs",
		PasswordHash: "x", Role: domain.RoleMember,
	}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Where("id = ?", user.ID).Delete(&UserModel{}) })

	session := &domain.UserSession{
		UserID: user.ID, Token: token, ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := repo.CreateSession(ctx, session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Where("token = ?", token).Delete(&UserSessionModel{}) })

	// Every path that takes a token: the hit, the miss, and the delete.
	if _, err := repo.GetSessionByToken(ctx, token); err != nil {
		t.Fatalf("get session: %v", err)
	}
	if _, err := repo.GetSessionByToken(ctx, uuid.New().String()); err == nil {
		t.Fatal("expected a miss for an unknown token")
	}
	if err := repo.DeleteSession(ctx, token); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	if out.Len() == 0 {
		t.Fatal("nothing was logged at all, so this test proves nothing")
	}
	if strings.Contains(out.String(), token) {
		t.Errorf("the session token reached the log:\n%s", out.String())
	}
	// The miss is ordinary traffic — an expired cookie, a signed-out browser —
	// and reporting it at error made routine behaviour look like a fault.
	if strings.Contains(out.String(), "level=error") {
		t.Errorf("a missing session was logged as an error:\n%s", out.String())
	}
}
