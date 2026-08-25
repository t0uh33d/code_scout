package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	dbadapter "github.com/getcodescout/code_scout/internal/adapters/db"
	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/live"
	"github.com/getcodescout/code_scout/internal/services"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"github.com/getcodescout/code_scout/pkg/sse"
	"github.com/getcodescout/code_scout/server"
	"github.com/getcodescout/code_scout/server/handlers"
	"github.com/getcodescout/code_scout/server/mcptools"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// This is the test that proves the whole pipe at once: a real router, real
// middleware, a real token in a real Authorization header, real services on a
// real Postgres, and the SDK's own client speaking genuine MCP over HTTP.
// Gated on CS_TEST_DB like every other integration test, so make test-all
// runs it.

type mcpWorld struct {
	ts       *httptest.Server
	db       *gorm.DB
	hub      *live.Hub
	tokenSvc *services.TokenService

	member   *domain.User // sees project
	outsider *domain.User // sees nothing
	project  *domain.Project
	secret   string // the project's SDK secret, which must never surface

	memberToken   string // plaintext PAT for member
	outsiderToken string
}

func newMCPWorld(t *testing.T) *mcpWorld {
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
	// The whole world shares this one pool; keep it small and close it, so
	// parallel packages cannot exhaust Postgres's connection slots.
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(8)
		t.Cleanup(func() { sqlDB.Close() })
	}
	if err := dbadapter.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// The same wiring as main.go, on the test database.
	projectRepo := dbadapter.NewProjectRepo(db)
	logRepo := dbadapter.NewLogRepo(db)
	userRepo := dbadapter.NewUserRepo(db)
	memberRepo := dbadapter.NewMemberRepo(db)
	sessionRepo := dbadapter.NewSessionRepo(db)
	tokenRepo := dbadapter.NewTokenRepo(db)
	usageRepo := dbadapter.NewUsageRepo(db)
	txMgr := dbadapter.NewTransactionManager(db)
	broker := sse.NewBroker()
	t.Cleanup(broker.Close)

	projectSvc := services.NewProjectService(projectRepo, memberRepo, txMgr)
	authSvc := services.NewAuthService(userRepo)
	memberSvc := services.NewMemberService(userRepo, memberRepo, tokenRepo, txMgr)
	tokenSvc := services.NewTokenService(tokenRepo, userRepo)
	settingsSvc := services.NewInstanceSettingsService(dbadapter.NewInstanceSettingsRepo(db))
	_ = settingsSvc.Load(context.Background())
	logSvc := services.NewLogService(logRepo, txMgr, broker, sessionRepo, usageRepo, settingsSvc)
	logQuerySvc := services.NewLogQueryService(logRepo, sessionRepo)
	versionSvc := services.NewVersionService(settingsSvc)

	hub := live.NewHub()
	t.Cleanup(hub.Close)

	srv := server.New(server.ServerOpts{
		DB:         db,
		ProjectSvc: projectSvc,
		AuthSvc:    authSvc,
		MemberSvc:  memberSvc,
		TokenSvc:   tokenSvc,
		MCPHandler: mcptools.NewHTTPHandler(mcptools.Deps{
			Logs:     logQuerySvc,
			Projects: projectSvc,
			Access:   memberSvc,
			Settings: settingsSvc,
			Live:     hub,
		}),
		ProjectHandler:          handlers.NewProjectHandler(projectSvc, memberSvc),
		LogHandler:              handlers.NewLogHandler(logSvc, settingsSvc),
		ViewHandler:             handlers.NewViewHandler(authSvc, projectSvc),
		AuthHandler:             handlers.NewAuthHandler(authSvc),
		DashboardHandler:        handlers.NewDashboardHandler(projectSvc, memberSvc),
		LogViewerHandler:        handlers.NewLogViewerHandler(logQuerySvc, projectSvc, broker, settingsSvc),
		ProjectSettingsHandler:  handlers.NewProjectSettingsHandler(projectSvc, memberSvc),
		MemberHandler:           handlers.NewMemberHandler(memberSvc, projectSvc),
		InstanceSettingsHandler: handlers.NewInstanceSettingsHandler(settingsSvc, memberSvc, projectSvc, versionSvc),
		AccountHandler:          handlers.NewAccountHandler(tokenSvc),
		ExportHandler:           handlers.NewExportHandler(logQuerySvc),
		LiveHandler:             handlers.NewLiveHandler(hub, projectSvc),
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	w := &mcpWorld{ts: ts, db: db, hub: hub, tokenSvc: tokenSvc}
	ctx := context.Background()

	seedUser := func(name string) *domain.User {
		u := &domain.User{
			Name: name, Email: name + "-" + uuid.NewString() + "@example.com",
			PasswordHash: "x", Role: domain.RoleMember,
		}
		if err := userRepo.Create(ctx, u); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
		t.Cleanup(func() {
			db.Unscoped().Where("user_id = ?", u.ID).Delete(&dbadapter.PersonalAccessTokenModel{})
			db.Unscoped().Where("id = ?", u.ID).Delete(&dbadapter.UserModel{})
		})
		return u
	}
	w.member = seedUser("mcp-member")
	w.outsider = seedUser("mcp-outsider")

	// A project with a secret, a membership for member only, and some logs.
	details, _, err := projectSvc.CreateProject(ctx, &domain.CreateProjectOpts{
		Name: "MCP IT " + uuid.NewString()[:8], Description: "integration", CreatedBy: w.member.ID,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	w.project = &domain.Project{ID: details.ID, Name: details.Name}
	w.secret = details.SecretKey
	t.Cleanup(func() {
		db.Unscoped().Where("project_id = ?", details.ID).Delete(&dbadapter.LogModel{})
		db.Unscoped().Where("project_id = ?", details.ID).Delete(&dbadapter.ProjectMemberModel{})
		db.Unscoped().Where("project_id = ?", details.ID).Delete(&dbadapter.ProjectSecretModel{})
		db.Unscoped().Where("id = ?", details.ID).Delete(&dbadapter.ProjectModel{})
	})
	if err := memberRepo.SetMembership(ctx, w.member.ID, details.ID, domain.LevelViewer); err != nil {
		t.Fatalf("membership: %v", err)
	}

	sessionID := uuid.New()
	logs := make([]domain.Log, 0, 5)
	base := time.Now().Add(-time.Hour)
	for i := range 5 {
		id, _ := uuid.NewV7()
		logs = append(logs, domain.Log{
			ID: id, ProjectID: details.ID, SessionID: sessionID,
			Level: "info", Message: "seeded log " + string(rune('A'+i)),
			TimeStamp: base.Add(time.Duration(i) * time.Minute),
		})
	}
	if _, err := logRepo.CreateBatch(ctx, logs); err != nil {
		t.Fatalf("seed logs: %v", err)
	}

	if w.memberToken, _, err = tokenSvc.Create(ctx, w.member.ID, "it-member", nil); err != nil {
		t.Fatalf("mint member token: %v", err)
	}
	if w.outsiderToken, _, err = tokenSvc.Create(ctx, w.outsider.ID, "it-outsider", nil); err != nil {
		t.Fatalf("mint outsider token: %v", err)
	}
	return w
}

// bearerTransport injects the token the way any MCP client config would.
type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func (w *mcpWorld) session(t *testing.T, token string) *mcp.ClientSession {
	t.Helper()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "it-client"}, nil).Connect(
		context.Background(),
		&mcp.StreamableClientTransport{
			Endpoint:   w.ts.URL + "/api/mcp",
			HTTPClient: &http.Client{Transport: bearerTransport{token: token}},
		}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func callText(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	var text strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	return res, text.String()
}

func TestMCPEndToEnd(t *testing.T) {
	w := newMCPWorld(t)

	// Everything under one world: seeding is the expensive part, and the
	// subtests read without stepping on each other.

	t.Run("RejectsABadTokenBeforeTheProtocol", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, w.ts.URL+"/api/mcp", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer csp_not-a-real-token")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", resp.StatusCode)
		}
		var parsed map[string]any
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatalf("401 body is not JSON: %s", body)
		}
		if code, _ := parsed["code"].(float64); int(code) != domain.ERR_TOKEN_INVALID_ERR_CODE {
			t.Errorf("code = %v, want %d", parsed["code"], domain.ERR_TOKEN_INVALID_ERR_CODE)
		}
		if strings.Contains(string(body), "jsonrpc") {
			t.Error("the refusal leaked a JSON-RPC envelope; auth must fail before the protocol")
		}
	})

	t.Run("InitializesAndListsTools", func(t *testing.T) {
		cs := w.session(t, w.memberToken)
		tools, err := cs.ListTools(context.Background(), nil)
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		if len(tools.Tools) != 18 {
			names := make([]string, 0, len(tools.Tools))
			for _, tool := range tools.Tools {
				names = append(names, tool.Name)
			}
			t.Errorf("expected 18 tools, got %d: %v", len(tools.Tools), names)
		}
	})

	t.Run("SearchLogsReturnsSeededLogsAndPaginates", func(t *testing.T) {
		cs := w.session(t, w.memberToken)

		res, text := callText(t, cs, "search_logs", map[string]any{
			"project_id": w.project.ID.String(), "limit": 3,
		})
		if res.IsError {
			t.Fatalf("search failed: %s", text)
		}
		var page1 struct {
			Logs       []struct{ ID, Message string } `json:"logs"`
			NextCursor string                         `json:"next_cursor"`
			HasMore    bool                           `json:"has_more"`
		}
		if err := json.Unmarshal([]byte(text), &page1); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(page1.Logs) != 3 || !page1.HasMore || page1.NextCursor == "" {
			t.Fatalf("first page wrong: %d logs, more=%v", len(page1.Logs), page1.HasMore)
		}

		_, text2 := callText(t, cs, "search_logs", map[string]any{
			"project_id": w.project.ID.String(), "limit": 3, "cursor": page1.NextCursor,
		})
		var page2 struct {
			Logs    []struct{ ID, Message string } `json:"logs"`
			HasMore bool                           `json:"has_more"`
		}
		if err := json.Unmarshal([]byte(text2), &page2); err != nil {
			t.Fatalf("decode page 2: %v", err)
		}
		if len(page2.Logs) != 2 || page2.HasMore {
			t.Errorf("second page wrong: %d logs, more=%v", len(page2.Logs), page2.HasMore)
		}
		seen := map[string]bool{}
		for _, l := range append(page1.Logs, page2.Logs...) {
			if seen[l.ID] {
				t.Errorf("log %s appeared on both pages", l.ID)
			}
			seen[l.ID] = true
		}
		if len(seen) != 5 {
			t.Errorf("expected all 5 seeded logs across two pages, got %d", len(seen))
		}
	})

	t.Run("AnInvisibleProjectIsIndistinguishableFromAbsent", func(t *testing.T) {
		cs := w.session(t, w.outsiderToken)

		resReal, textReal := callText(t, cs, "search_logs", map[string]any{"project_id": w.project.ID.String()})
		resFake, textFake := callText(t, cs, "search_logs", map[string]any{"project_id": uuid.NewString()})
		if !resReal.IsError || !resFake.IsError {
			t.Fatal("an outsider read a project")
		}
		if textReal != textFake {
			t.Errorf("a real-but-invisible project answers differently from an absent one: %q vs %q", textReal, textFake)
		}
	})

	t.Run("SearchParseErrorCarriesThePosition", func(t *testing.T) {
		cs := w.session(t, w.memberToken)
		res, text := callText(t, cs, "search_logs", map[string]any{
			"project_id": w.project.ID.String(), "query": "level:nope",
		})
		if !res.IsError {
			t.Fatal("a bad query did not error")
		}
		if !strings.Contains(text, "position") {
			t.Errorf("the parse error should carry its position: %q", text)
		}
	})

	t.Run("ToolOutputNeverContainsSecrets", func(t *testing.T) {
		cs := w.session(t, w.memberToken)
		var all strings.Builder
		for tool, args := range map[string]map[string]any{
			"list_projects":        {},
			"get_project_overview": {"project_id": w.project.ID.String()},
			"search_logs":          {"project_id": w.project.ID.String()},
			"get_error_groups":     {"project_id": w.project.ID.String()},
			"get_tag_counts":       {"project_id": w.project.ID.String()},
			"list_sessions":        {"project_id": w.project.ID.String()},
			"list_devices":         {"project_id": w.project.ID.String()},
			"list_network_calls":   {"project_id": w.project.ID.String()},
			"list_live_sessions":   {"project_id": w.project.ID.String()},
		} {
			_, text := callText(t, cs, tool, args)
			all.WriteString(text)
		}
		for name, secret := range map[string]string{
			"project secret":  w.secret,
			"password hash":   "password_hash",
			"member token":    w.memberToken,
			"outsider token":  w.outsiderToken,
			"token hash":      domain.HashPersonalToken(w.memberToken),
		} {
			if secret != "" && strings.Contains(all.String(), secret) {
				t.Errorf("%s appeared in tool output", name)
			}
		}
	})

	t.Run("ResponsesAreSingleJSONBodies", func(t *testing.T) {
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
		req, _ := http.NewRequest(http.MethodPost, w.ts.URL+"/api/mcp", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+w.memberToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")

		start := time.Now()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)

		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if !strings.Contains(string(raw), `"tools"`) {
			t.Errorf("expected a tools/list result, got: %.200s", raw)
		}
		if took := time.Since(start); took > 10*time.Second {
			t.Errorf("a stateless call took %v; nothing may approach the 30s WriteTimeout", took)
		}
	})
}

// The token in the Authorization header crosses the logger's path on every
// request (HttpLogger runs outermost). This drives the real middleware with
// the real logger captured at debug and asserts the credential never lands.
func TestThePersonalTokenNeverReachesTheServerLog(t *testing.T) {
	w := newMCPWorld(t)

	logger := cslog.GetLogger()
	oldOut, oldLevel := logger.Out, logger.Level
	t.Cleanup(func() { logger.SetOutput(oldOut); logger.SetLevel(oldLevel) })
	var out bytes.Buffer
	logger.SetOutput(&out)
	logger.SetLevel(logrus.DebugLevel)

	cs := w.session(t, w.memberToken)
	if _, err := cs.ListTools(context.Background(), nil); err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if _, text := callText(t, cs, "search_logs", map[string]any{"project_id": w.project.ID.String()}); text == "" {
		t.Fatal("no output at all")
	}
	// A failed authentication too: the miss path logs, and must not log this.
	req, _ := http.NewRequest(http.MethodPost, w.ts.URL+"/api/mcp", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer csp_wrong-on-purpose-aaaaaaaaaaaaaaaaaaaaaaa")
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
	}

	if out.Len() == 0 {
		t.Fatal("nothing was logged at all, so this test proves nothing")
	}
	logged := out.String()
	for name, secret := range map[string]string{
		"member token plaintext": w.memberToken,
		"member token hash":      domain.HashPersonalToken(w.memberToken),
		"the wrong bearer":       "csp_wrong-on-purpose",
	} {
		if strings.Contains(logged, secret) {
			t.Errorf("%s reached the server log", name)
		}
	}
}
