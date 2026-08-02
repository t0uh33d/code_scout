package services_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	dbadapter "github.com/t0uh33d/code_scout/internal/adapters/db"
	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/internal/services"
	"gorm.io/gorm"
)

func memberSvc(t *testing.T, db *gorm.DB) *services.MemberService {
	t.Helper()
	return services.NewMemberService(
		dbadapter.NewUserRepo(db),
		dbadapter.NewMemberRepo(db),
		dbadapter.NewTransactionManager(db),
	)
}

func seedRole(t *testing.T, db *gorm.DB, role domain.Role) *domain.User {
	t.Helper()
	repo := dbadapter.NewUserRepo(db)
	u := &domain.User{
		Name:         string(role) + "-" + uuid.NewString()[:8],
		Email:        strings.ToLower(uuid.NewString()) + "@test.local",
		Role:         role,
		PasswordHash: "x",
	}
	if err := repo.Create(context.Background(), u); err != nil {
		t.Fatalf("seed %s: %v", role, err)
	}
	t.Cleanup(func() {
		db.Unscoped().Where("user_id = ?", u.ID).Delete(&dbadapter.ProjectMemberModel{})
		db.Unscoped().Where("user_id = ?", u.ID).Delete(&dbadapter.UserSessionModel{})
		db.Unscoped().Where("id = ?", u.ID).Delete(&dbadapter.UserModel{})
	})
	return u
}

// TestLastSuperAdminCannotBeRemoved is the guard that keeps an instance
// administrable. Without it, removing the only super admin bricks the install.
func TestLastSuperAdminCannotBeRemoved(t *testing.T) {
	db := authTestDB(t)
	svc := memberSvc(t, db)
	ctx := context.Background()

	// Clear the field so "last" means last.
	db.Unscoped().Where("role = ?", "super_admin").Delete(&dbadapter.UserModel{})

	solo := seedRole(t, db, domain.RoleSuperAdmin)

	status, err := svc.DeleteMember(ctx, solo, solo.ID)
	if err == nil {
		t.Fatal("removing the last super admin should be refused")
	}
	if status != 409 {
		t.Errorf("status = %d, want 409", status)
	}

	// Demotion is the same hole by another name.
	if status, err := svc.ChangeRole(ctx, solo, solo.ID, domain.RoleAdmin); err == nil {
		t.Errorf("demoting the last super admin should be refused, got status %d", status)
	}

	// With a second one, both become removable.
	second := seedRole(t, db, domain.RoleSuperAdmin)
	if _, err := svc.DeleteMember(ctx, second, solo.ID); err != nil {
		t.Fatalf("with two super admins, removal should succeed: %v", err)
	}
}

// TestConcurrentSuperAdminRemovalKeepsOne is the race the row lock exists for:
// two super admins removing each other at the same moment must not both
// succeed, or the instance is left with nobody who can administer it.
//
// Both goroutines are released from a shared barrier so their transactions
// genuinely overlap. Repeated, because a single round can serialise by luck and
// pass against a broken implementation.
func TestConcurrentSuperAdminRemovalKeepsOne(t *testing.T) {
	db := authTestDB(t)
	svc := memberSvc(t, db)
	ctx := context.Background()

	for round := range 15 {
		db.Unscoped().Where("role = ?", "super_admin").Delete(&dbadapter.UserModel{})
		a := seedRole(t, db, domain.RoleSuperAdmin)
		b := seedRole(t, db, domain.RoleSuperAdmin)

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); <-start; svc.DeleteMember(ctx, a, b.ID) }()
		go func() { defer wg.Done(); <-start; svc.DeleteMember(ctx, b, a.ID) }()
		close(start)
		wg.Wait()

		var remaining int64
		db.Model(&dbadapter.UserModel{}).Where("role = ?", "super_admin").Count(&remaining)
		if remaining < 1 {
			t.Fatalf("round %d: both removals committed, leaving no super admin", round)
		}
	}
}

func TestAdminCannotManageAnotherAdmin(t *testing.T) {
	db := authTestDB(t)
	svc := memberSvc(t, db)
	ctx := context.Background()

	actor := seedRole(t, db, domain.RoleAdmin)
	peer := seedRole(t, db, domain.RoleAdmin)
	member := seedRole(t, db, domain.RoleMember)

	if _, status, err := svc.ResetMemberPassword(ctx, actor, peer.ID); err == nil {
		t.Errorf("an admin must not reset another admin's password, got status %d", status)
	}
	if _, _, err := svc.ResetMemberPassword(ctx, actor, member.ID); err != nil {
		t.Errorf("an admin should be able to reset a member's password: %v", err)
	}
	if status, err := svc.ChangeRole(ctx, actor, member.ID, domain.RoleAdmin); err == nil {
		t.Errorf("only a super admin changes roles, got status %d", status)
	}
}

func TestResetPasswordKillsSessionsAndForcesChange(t *testing.T) {
	db := authTestDB(t)
	svc := memberSvc(t, db)
	users := dbadapter.NewUserRepo(db)
	ctx := context.Background()

	admin := seedRole(t, db, domain.RoleAdmin)
	target := seedRole(t, db, domain.RoleMember)

	if err := users.CreateSession(ctx, &domain.UserSession{
		UserID: target.ID, Token: uuid.NewString(), ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	password, _, err := svc.ResetMemberPassword(ctx, admin, target.ID)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if len(password) == 0 {
		t.Fatal("expected a temporary password")
	}

	var sessions int64
	db.Model(&dbadapter.UserSessionModel{}).Where("user_id = ?", target.ID).Count(&sessions)
	if sessions != 0 {
		t.Errorf("sessions after reset = %d, want 0", sessions)
	}

	reloaded, err := users.GetByID(ctx, target.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reloaded.MustChangePassword {
		t.Error("a reset account should be forced to pick a new password")
	}
}

func TestCreateMemberRoleCeiling(t *testing.T) {
	db := authTestDB(t)
	svc := memberSvc(t, db)
	ctx := context.Background()

	admin := seedRole(t, db, domain.RoleAdmin)
	sa := seedRole(t, db, domain.RoleSuperAdmin)

	// An admin minting another admin would widen the set of account creators
	// sideways, so it is refused.
	if _, _, status, err := svc.CreateMember(ctx, admin, &domain.CreateMemberOpts{
		Name: "Nope", Email: "nope-" + uuid.NewString() + "@test.local", Role: domain.RoleAdmin,
	}); err == nil {
		t.Errorf("an admin must not create an admin, got status %d", status)
	}

	created, temp, _, err := svc.CreateMember(ctx, admin, &domain.CreateMemberOpts{
		Name: "Fine", Email: "fine-" + uuid.NewString() + "@test.local", Role: domain.RoleMember,
	})
	if err != nil {
		t.Fatalf("an admin should be able to create a member: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Where("id = ?", created.ID).Delete(&dbadapter.UserModel{}) })
	if temp == "" {
		t.Error("expected a generated temporary password")
	}
	if !created.MustChangePassword {
		t.Error("a generated password should force a change on first sign in")
	}

	// A super admin may create an admin.
	created2, _, _, err := svc.CreateMember(ctx, sa, &domain.CreateMemberOpts{
		Name: "Admin", Email: "admin-" + uuid.NewString() + "@test.local", Role: domain.RoleAdmin,
	})
	if err != nil {
		t.Fatalf("a super admin should be able to create an admin: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Where("id = ?", created2.ID).Delete(&dbadapter.UserModel{}) })
}

// TestProjectVisibilityIsScopedToMembership is the headline behaviour of this
// whole slice: an admin sees the projects they are on and nothing else, while a
// super admin sees the instance whole.
func TestProjectVisibilityIsScopedToMembership(t *testing.T) {
	db := authTestDB(t)
	psvc := projectSvc(t, db)
	ctx := context.Background()

	owner := seedRole(t, db, domain.RoleAdmin)
	stranger := seedRole(t, db, domain.RoleAdmin)
	sa := seedRole(t, db, domain.RoleSuperAdmin)

	details, _, err := psvc.CreateProject(ctx, &domain.CreateProjectOpts{
		Name: "scoped-" + uuid.NewString(), CreatedBy: owner.ID,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		db.Unscoped().Where("project_id = ?", details.ID).Delete(&dbadapter.ProjectMemberModel{})
		db.Unscoped().Where("project_id = ?", details.ID).Delete(&dbadapter.ProjectSecretModel{})
		db.Unscoped().Where("id = ?", details.ID).Delete(&dbadapter.ProjectModel{})
	})

	sees := func(u *domain.User) bool {
		opts := domain.ProjectListOpts{Page: 1, PageSize: 200}
		opts.ScopeToUser(u)
		res, _, err := psvc.ListProjects(ctx, opts)
		if err != nil || res == nil {
			t.Fatalf("list for %s: %v", u.Role, err)
		}
		for _, item := range res.Items {
			if item.ID == details.ID {
				return true
			}
		}
		return false
	}

	if !sees(owner) {
		t.Error("the creator must see the project they made")
	}
	if sees(stranger) {
		t.Error("an admin must NOT see a project someone else created")
	}
	if !sees(sa) {
		t.Error("a super admin sees every project")
	}

	// Nil user is the fail-closed case: no scoping information means nothing.
	if sees(nil) {
		t.Error("an unauthenticated caller must see nothing")
	}
}

// TestCreateMemberCannotGrantUnmanagedProjects is the regression for a hole the
// review found: project ids come from a form, and without this check an admin
// could grant a brand new account access to a project they cannot see, then
// sign in as that account.
func TestCreateMemberCannotGrantUnmanagedProjects(t *testing.T) {
	db := authTestDB(t)
	svc := memberSvc(t, db)
	psvc := projectSvc(t, db)
	ctx := context.Background()

	owner := seedRole(t, db, domain.RoleAdmin)
	outsider := seedRole(t, db, domain.RoleAdmin)

	secret, _, err := psvc.CreateProject(ctx, &domain.CreateProjectOpts{
		Name: "unmanaged-" + uuid.NewString(), CreatedBy: owner.ID,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		db.Unscoped().Where("project_id = ?", secret.ID).Delete(&dbadapter.ProjectMemberModel{})
		db.Unscoped().Where("project_id = ?", secret.ID).Delete(&dbadapter.ProjectSecretModel{})
		db.Unscoped().Where("id = ?", secret.ID).Delete(&dbadapter.ProjectModel{})
	})

	email := "grant-" + uuid.NewString() + "@test.local"
	_, _, status, err := svc.CreateMember(ctx, outsider, &domain.CreateMemberOpts{
		Name: "Sneaky", Email: email, Role: domain.RoleMember,
		Projects: []domain.ProjectAssignment{{ProjectID: secret.ID, Level: domain.LevelMaintainer}},
	})
	if err == nil {
		t.Fatal("an admin must not grant access to a project they do not manage")
	}
	if status != 403 {
		t.Errorf("status = %d, want 403", status)
	}

	// And the account must not exist: a refused grant cannot leave a half
	// created user behind.
	var count int64
	db.Unscoped().Model(&dbadapter.UserModel{}).Where("email = ?", email).Count(&count)
	if count != 0 {
		t.Fatalf("a refused grant created %d accounts, want 0", count)
	}
}

// TestDeletedAccountFreesItsEmail is the regression for a soft delete that left
// the unique index holding the address forever.
func TestDeletedAccountFreesItsEmail(t *testing.T) {
	db := authTestDB(t)
	svc := memberSvc(t, db)
	ctx := context.Background()

	sa := seedRole(t, db, domain.RoleSuperAdmin)
	email := "recycle-" + uuid.NewString() + "@test.local"

	first, _, _, err := svc.CreateMember(ctx, sa, &domain.CreateMemberOpts{
		Name: "First", Email: email, Role: domain.RoleMember,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.DeleteMember(ctx, sa, first.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	second, _, _, err := svc.CreateMember(ctx, sa, &domain.CreateMemberOpts{
		Name: "Second", Email: email, Role: domain.RoleMember,
	})
	if err != nil {
		t.Fatalf("the address should be reusable after removal: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Where("id = ?", second.ID).Delete(&dbadapter.UserModel{}) })
}

func TestProjectAccessRequiresMaintainer(t *testing.T) {
	db := authTestDB(t)
	svc := memberSvc(t, db)
	psvc := projectSvc(t, db)
	members := dbadapter.NewMemberRepo(db)
	ctx := context.Background()

	owner := seedRole(t, db, domain.RoleAdmin)
	viewer := seedRole(t, db, domain.RoleMember)
	outsider := seedRole(t, db, domain.RoleAdmin)

	details, _, err := psvc.CreateProject(ctx, &domain.CreateProjectOpts{
		Name: "access-" + uuid.NewString(), CreatedBy: owner.ID,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		db.Unscoped().Where("project_id = ?", details.ID).Delete(&dbadapter.ProjectMemberModel{})
		db.Unscoped().Where("project_id = ?", details.ID).Delete(&dbadapter.ProjectSecretModel{})
		db.Unscoped().Where("id = ?", details.ID).Delete(&dbadapter.ProjectModel{})
	})

	// The creator is a maintainer, or they would lose sight of what they made.
	m, err := members.GetMembership(ctx, owner.ID, details.ID)
	if err != nil || m.Level != domain.LevelMaintainer {
		t.Fatalf("creator should be a maintainer, got %v (err %v)", m, err)
	}

	if _, err := svc.SetProjectAccess(ctx, owner, details.ID, viewer.ID, domain.LevelViewer); err != nil {
		t.Fatalf("maintainer should be able to grant access: %v", err)
	}

	// A viewer cannot hand out access.
	if status, err := svc.SetProjectAccess(ctx, viewer, details.ID, outsider.ID, domain.LevelViewer); err == nil {
		t.Errorf("a viewer must not grant access, got status %d", status)
	}

	// An admin who is not on the project gets 404, not 403: they should not
	// learn that the project exists.
	status, err := svc.SetProjectAccess(ctx, outsider, details.ID, viewer.ID, domain.LevelViewer)
	if err == nil {
		t.Fatal("an outsider must not grant access")
	}
	if status != 404 {
		t.Errorf("outsider status = %d, want 404 so the project's existence is not disclosed", status)
	}

	// Super admins are not membership rows.
	sa := seedRole(t, db, domain.RoleSuperAdmin)
	if _, err := svc.SetProjectAccess(ctx, owner, details.ID, sa.ID, domain.LevelViewer); err == nil {
		t.Error("adding a super admin as a member should be refused, their access is not revocable")
	}
}
