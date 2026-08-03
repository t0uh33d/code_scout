package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/getcodescout/code_scout/internal/domain"
	"gorm.io/gorm"
)

// Session-scoped filters are the only ones that are not columns on `logs`, so
// they are the only ones whose SQL can be wrong in a way the parser tests
// cannot see. These drive the real query against real Postgres.

// scopeFixture is one project with three launches that differ in every way a
// session filter can narrow on.
type scopeFixture struct {
	projectID uuid.UUID
	logs      *LogRepo

	aliceInstall uuid.UUID
	bobInstall   uuid.UUID
}

func seedScopeFixture(t *testing.T, db *gorm.DB) scopeFixture {
	t.Helper()

	projectID := seedProject(t, db)
	sessions := NewSessionRepo(db)
	logs := NewLogRepo(db)
	ctx := context.Background()

	fx := scopeFixture{
		projectID:    projectID,
		logs:         logs,
		aliceInstall: uuid.New(),
		bobInstall:   uuid.New(),
	}

	now := time.Now()
	type launch struct {
		install    uuid.UUID
		user       *string
		device     string
		osName     string
		osVersion  string
		appVersion string
		message    string
	}

	alice := "u_alice"
	bob := "u_bob"

	launches := []launch{
		{fx.aliceInstall, &alice, "Pixel 7", "Android", "14", "3.11.2", "alice on the pixel"},
		{fx.aliceInstall, &alice, "Pixel 7", "Android", "14", "3.12.0", "alice after upgrading"},
		{fx.bobInstall, &bob, "iPhone 15 Pro", "iOS", "17.4", "3.11.2", "bob on the iphone"},
		// Anonymous, so `user:` must not match it and it must still appear
		// unfiltered.
		{uuid.New(), nil, "Galaxy S23", "Android", "13", "3.10.0", "nobody signed in"},
	}

	for _, l := range launches {
		install := l.install
		sessionID := uuid.New()
		err := sessions.Upsert(ctx, &domain.Session{
			ID: sessionID, ProjectID: projectID,
			InstallationID: &install, UserID: l.user,
			DeviceModel: strp(l.device), OSName: strp(l.osName), OSVersion: strp(l.osVersion),
			AppVersion: strp(l.appVersion), BuildNumber: strp("418"),
			StartedAt: now, LastSeenAt: now,
		})
		if err != nil {
			t.Fatalf("seed session: %v", err)
		}

		entry := taggedLog(projectID, l.message, "info", nil)
		entry.SessionID = sessionID
		if _, err := logs.CreateBatch(ctx, []domain.Log{entry}); err != nil {
			t.Fatalf("seed log: %v", err)
		}
	}

	return fx
}

func (fx scopeFixture) list(t *testing.T, scope domain.SessionScope) []string {
	t.Helper()
	return listWith(t, fx.logs, fx.projectID, domain.SearchFilter{Session: scope})
}

func TestScopeByUser(t *testing.T) {
	db := testDB(t)
	fx := seedScopeFixture(t, db)

	got := fx.list(t, domain.SessionScope{User: "u_alice"})
	if len(got) != 2 {
		t.Fatalf("want alice's two launches, got %v", got)
	}
	for _, m := range got {
		if m == "bob on the iphone" || m == "nobody signed in" {
			t.Errorf("user:u_alice returned %q", m)
		}
	}
}

// An opaque id must match exactly. A prefix match would put `u_alice2` in
// `u_alice`'s results, which is the wrong person rather than an extra row.
func TestScopeByUserIsExact(t *testing.T) {
	db := testDB(t)
	fx := seedScopeFixture(t, db)

	if got := fx.list(t, domain.SessionScope{User: "u_alic"}); len(got) != 0 {
		t.Errorf("a partial user id matched: %v", got)
	}
	if got := fx.list(t, domain.SessionScope{User: "alice"}); len(got) != 0 {
		t.Errorf("a substring of a user id matched: %v", got)
	}
}

// Nobody types "Pixel 7" when they mean every Pixel.
func TestScopeByDeviceIsPartialAndCaseInsensitive(t *testing.T) {
	db := testDB(t)
	fx := seedScopeFixture(t, db)

	for _, typed := range []string{"Pixel", "pixel", "PIXEL 7"} {
		got := fx.list(t, domain.SessionScope{Device: typed})
		if len(got) != 2 {
			t.Errorf("device:%q returned %v, want alice's two launches", typed, got)
		}
	}
}

// The row shows "Android 14" as one string, so both halves of it have to work.
func TestScopeByOSMatchesNameOrVersion(t *testing.T) {
	db := testDB(t)
	fx := seedScopeFixture(t, db)

	if got := fx.list(t, domain.SessionScope{OS: "iOS"}); len(got) != 1 {
		t.Errorf("os:iOS returned %v, want bob's launch", got)
	}
	if got := fx.list(t, domain.SessionScope{OS: "17.4"}); len(got) != 1 {
		t.Errorf("os:17.4 returned %v, want bob's launch", got)
	}
	if got := fx.list(t, domain.SessionScope{OS: "android"}); len(got) != 3 {
		t.Errorf("os:android returned %v, want the three Android launches", got)
	}
}

// The load-bearing one for version filtering. Prefix matching would be handy
// and treacherous: app_version:3.1 quietly including 3.11.2 and 3.12.0 is a
// wrong answer nobody checks.
func TestScopeByAppVersionIsExact(t *testing.T) {
	db := testDB(t)
	fx := seedScopeFixture(t, db)

	got := fx.list(t, domain.SessionScope{AppVersion: "3.11.2"})
	if len(got) != 2 {
		t.Fatalf("app_version:3.11.2 returned %v, want alice's first and bob's", got)
	}

	if partial := fx.list(t, domain.SessionScope{AppVersion: "3.1"}); len(partial) != 0 {
		t.Errorf("app_version:3.1 matched %v — a prefix must not match", partial)
	}
}

func TestScopeByInstallation(t *testing.T) {
	db := testDB(t)
	fx := seedScopeFixture(t, db)

	got := fx.list(t, domain.SessionScope{Installation: fx.aliceInstall.String()})
	if len(got) != 2 {
		t.Errorf("want both launches from that install, got %v", got)
	}
}

// Two session filters are an AND. Alice has two launches, one per version, so
// combining them must leave exactly one.
func TestScopeFiltersCombine(t *testing.T) {
	db := testDB(t)
	fx := seedScopeFixture(t, db)

	got := fx.list(t, domain.SessionScope{User: "u_alice", AppVersion: "3.12.0"})
	if len(got) != 1 || got[0] != "alice after upgrading" {
		t.Errorf("want only the launch matching both, got %v", got)
	}
}

// A session filter has to compose with the ordinary log filters rather than
// replacing them.
func TestScopeCombinesWithLogFilters(t *testing.T) {
	db := testDB(t)
	fx := seedScopeFixture(t, db)
	ctx := context.Background()

	// One more log on a brand new session belonging to alice, at error level.
	sessions := NewSessionRepo(db)
	sessionID := uuid.New()
	install := fx.aliceInstall
	alice := "u_alice"
	if err := sessions.Upsert(ctx, &domain.Session{
		ID: sessionID, ProjectID: fx.projectID,
		InstallationID: &install, UserID: &alice,
		DeviceModel: strp("Pixel 7"), OSName: strp("Android"), OSVersion: strp("14"),
		AppVersion: strp("3.12.0"), BuildNumber: strp("418"),
		StartedAt: time.Now(), LastSeenAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	entry := taggedLog(fx.projectID, "alice hit an error", "error", nil)
	entry.SessionID = sessionID
	if _, err := fx.logs.CreateBatch(ctx, []domain.Log{entry}); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	got := listWith(t, fx.logs, fx.projectID, domain.SearchFilter{
		Levels:  []string{"error"},
		Session: domain.SessionScope{User: "u_alice"},
	})
	if len(got) != 1 || got[0] != "alice hit an error" {
		t.Errorf("want alice's error only, got %v", got)
	}
}

// An anonymous session has no user, and `user:` must not sweep it up — a NULL
// comparison written the obvious way would.
func TestScopeExcludesAnonymousSessions(t *testing.T) {
	db := testDB(t)
	fx := seedScopeFixture(t, db)

	all := fx.list(t, domain.SessionScope{})
	if len(all) != 4 {
		t.Fatalf("unfiltered should show every launch, got %v", all)
	}

	for _, m := range fx.list(t, domain.SessionScope{User: "u_alice"}) {
		if m == "nobody signed in" {
			t.Error("an anonymous session matched a user filter")
		}
	}
}

// The subquery itself is project-scoped, tested directly rather than through
// List.
//
// Going through List would pass either way, because the outer query already
// filters logs by project — so this is the only place that can tell whether the
// guard in the subquery is actually there. It matters because session ids come
// from the client: "no two projects share one" is an assumption about somebody
// else's uuid generator, not a property we enforce.
func TestScopeSubqueryIsProjectScoped(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	mine := seedProject(t, db)
	theirs := seedProject(t, db)
	sessions := NewSessionRepo(db)

	alice := "u_alice"
	install := uuid.New()
	mySession := uuid.New()
	theirSession := uuid.New()

	for id, project := range map[uuid.UUID]uuid.UUID{mySession: mine, theirSession: theirs} {
		if err := sessions.Upsert(ctx, &domain.Session{
			ID: id, ProjectID: project,
			InstallationID: &install, UserID: &alice,
			DeviceModel: strp("Pixel 7"), OSName: strp("Android"), OSVersion: strp("14"),
			AppVersion: strp("3.11.2"), BuildNumber: strp("418"),
			StartedAt: time.Now(), LastSeenAt: time.Now(),
		}); err != nil {
			t.Fatalf("seed session: %v", err)
		}
	}

	sub := sessionScopeSubquery(db, mine, domain.SessionScope{User: alice})
	if sub == nil {
		t.Fatal("a set scope should produce a subquery")
	}

	var ids []uuid.UUID
	if err := sub.Find(&ids).Error; err != nil {
		t.Fatalf("run subquery: %v", err)
	}

	for _, id := range ids {
		if id == theirSession {
			t.Fatal("the subquery returned another project's session")
		}
	}
	var found bool
	for _, id := range ids {
		if id == mySession {
			found = true
		}
	}
	if !found {
		t.Error("the subquery did not return this project's own session")
	}
}

// The same property observed through List, which is what a user actually
// experiences. Passes with or without the subquery's own project filter, since
// the outer query scopes logs by project — kept because it is the behaviour
// that matters, not because it pins the implementation.
func TestScopeIsProjectScoped(t *testing.T) {
	db := testDB(t)
	mine := seedScopeFixture(t, db)

	// A second project with a session carrying the same user id.
	theirProject := seedProject(t, db)
	sessions := NewSessionRepo(db)
	ctx := context.Background()

	theirSession := uuid.New()
	install := uuid.New()
	alice := "u_alice"
	if err := sessions.Upsert(ctx, &domain.Session{
		ID: theirSession, ProjectID: theirProject,
		InstallationID: &install, UserID: &alice,
		DeviceModel: strp("Pixel 7"), OSName: strp("Android"), OSVersion: strp("14"),
		AppVersion: strp("3.11.2"), BuildNumber: strp("418"),
		StartedAt: time.Now(), LastSeenAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	theirLog := taggedLog(theirProject, "another project's secret", "info", nil)
	theirLog.SessionID = theirSession
	if _, err := mine.logs.CreateBatch(ctx, []domain.Log{theirLog}); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	for _, m := range mine.list(t, domain.SessionScope{User: "u_alice"}) {
		if m == "another project's secret" {
			t.Fatal("a session filter reached into another project")
		}
	}
}

// Something that matches no session returns nothing, rather than falling back
// to every log in the project — which is what an unapplied filter would do.
func TestScopeWithNoMatchesReturnsNothing(t *testing.T) {
	db := testDB(t)
	fx := seedScopeFixture(t, db)

	if got := fx.list(t, domain.SessionScope{User: "u_nobody"}); len(got) != 0 {
		t.Errorf("an unmatched user returned %v", got)
	}
	if got := fx.list(t, domain.SessionScope{Device: "Nokia 3310"}); len(got) != 0 {
		t.Errorf("an unmatched device returned %v", got)
	}
}
