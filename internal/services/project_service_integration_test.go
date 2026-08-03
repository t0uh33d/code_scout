package services_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	dbadapter "github.com/getcodescout/code_scout/internal/adapters/db"
	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/internal/services"
	"github.com/getcodescout/code_scout/pkg/utils"
	"gorm.io/gorm"
)

// These cover the settings screen's service layer against a real Postgres,
// because the behaviour under test is transactional: row locks, cascading
// hard deletes, and whether a rollback leaves a project half-deleted. Skips
// without CS_TEST_DB, same as the other integration tests.

func projectSvc(t *testing.T, db *gorm.DB) *services.ProjectService {
	t.Helper()
	return services.NewProjectService(
		dbadapter.NewProjectRepo(db),
		dbadapter.NewMemberRepo(db),
		dbadapter.NewTransactionManager(db),
	)
}

// assertFieldError checks the error carries a per-field message the settings
// form can render against the offending input. Before this work, field errors
// were built but thrown away before they escaped the service.
func assertFieldError(t *testing.T, err error, field string) {
	t.Helper()
	var appErr *utils.ErrorJson
	if !errors.As(err, &appErr) {
		t.Fatalf("error %v does not unwrap to *utils.ErrorJson", err)
	}
	for _, fe := range appErr.Errors {
		if fe.Field == field {
			if fe.Detail == "" {
				t.Errorf("field error for %q has no detail message", field)
			}
			return
		}
	}
	t.Fatalf("no field error for %q, got %+v", field, appErr.Errors)
}

// newProject creates a project through the service and cleans it up.
func newProject(t *testing.T, db *gorm.DB, svc *services.ProjectService, name string) *domain.ProjectDetails {
	t.Helper()
	details, _, err := svc.CreateProject(context.Background(), &domain.CreateProjectOpts{
		Name: name, Description: "integration",
	})
	if err != nil {
		t.Fatalf("create project %q: %v", name, err)
	}
	t.Cleanup(func() {
		db.Unscoped().Where("project_id = ?", details.ID).Delete(&dbadapter.ProjectSecretModel{})
		db.Unscoped().Where("project_id = ?", details.ID).Delete(&dbadapter.ProjectFavoriteModel{})
		db.Unscoped().Where("id = ?", details.ID).Delete(&dbadapter.ProjectModel{})
	})
	return details
}

// TestDeleteProjectWithNoSecretSucceeds is the regression test for the bug this
// work exists to fix: DeleteProject read the secret, got ErrRecordNotFound for a
// project that had none, and returned it, rolling back the project delete that
// had already happened in the same transaction. The caller saw a 500 and the
// project was still there.
func TestDeleteProjectWithNoSecretSucceeds(t *testing.T) {
	db := authTestDB(t)
	svc := projectSvc(t, db)
	ctx := context.Background()

	// Inserted directly so it never gets a secret row.
	model := dbadapter.ProjectModel{Name: "no-secret-" + uuid.NewString(), Description: "integration"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Where("id = ?", model.ID).Delete(&dbadapter.ProjectModel{}) })

	if _, err := svc.DeleteProject(ctx, model.ID); err != nil {
		t.Fatalf("delete project with no secret: %v", err)
	}

	// Asserting only on the error would pass against a half-fix that swallowed
	// the error but still rolled back, so check the row actually went.
	var live dbadapter.ProjectModel
	if err := db.Where("id = ?", model.ID).First(&live).Error; err == nil {
		t.Fatal("project is still live after a successful delete")
	}
	var deleted dbadapter.ProjectModel
	if err := db.Unscoped().Where("id = ?", model.ID).First(&deleted).Error; err != nil {
		t.Fatalf("project row vanished entirely, expected a soft delete: %v", err)
	}
	if !deleted.DeletedAt.Valid {
		t.Fatal("project row has no deleted_at, so the transaction rolled back")
	}
}

// TestRotateSecretInvalidatesTheOldSecret drives the assertion through
// ValidateProjectCredentials on purpose: that is the function
// middleware.Authenticate calls on every log upload, so this proves the
// user-visible property rather than an implementation detail.
func TestRotateSecretInvalidatesTheOldSecret(t *testing.T) {
	db := authTestDB(t)
	svc := projectSvc(t, db)
	ctx := context.Background()

	details := newProject(t, db, svc, "rotate-"+uuid.NewString())
	oldKey := details.SecretKey
	if oldKey == "" {
		t.Fatal("create did not return a secret")
	}

	if _, _, err := svc.ValidateProjectCredentials(ctx, details.ID, oldKey); err != nil {
		t.Fatalf("the original secret should authenticate: %v", err)
	}

	newKey, _, err := svc.RotateSecret(ctx, details.ID)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if newKey == oldKey {
		t.Fatal("rotation returned the same key")
	}

	if _, _, err := svc.ValidateProjectCredentials(ctx, details.ID, oldKey); err == nil {
		t.Fatal("the old secret still authenticates after rotation")
	}
	if _, _, err := svc.ValidateProjectCredentials(ctx, details.ID, newKey); err != nil {
		t.Fatalf("the new secret should authenticate: %v", err)
	}

	// Exactly one row survives, counting tombstones: two live rows would make
	// GetSecret's ORDER BY pick one arbitrarily.
	var count int64
	db.Unscoped().Model(&dbadapter.ProjectSecretModel{}).
		Where("project_id = ?", details.ID).Count(&count)
	if count != 1 {
		t.Fatalf("secret rows after rotation = %d, want exactly 1", count)
	}
}

func TestRotateSecretRepairsAProjectWithNoSecret(t *testing.T) {
	db := authTestDB(t)
	svc := projectSvc(t, db)
	ctx := context.Background()

	details := newProject(t, db, svc, "repair-"+uuid.NewString())
	// Strip the secret to simulate the broken state rotation must recover from.
	db.Unscoped().Where("project_id = ?", details.ID).Delete(&dbadapter.ProjectSecretModel{})

	newKey, _, err := svc.RotateSecret(ctx, details.ID)
	if err != nil {
		t.Fatalf("rotate on a project with no secret: %v", err)
	}
	if _, _, err := svc.ValidateProjectCredentials(ctx, details.ID, newKey); err != nil {
		t.Fatalf("the minted secret should authenticate: %v", err)
	}
}

func TestDeleteProjectRemovesSecretsAndFavorites(t *testing.T) {
	db := authTestDB(t)
	repo := dbadapter.NewProjectRepo(db)
	svc := projectSvc(t, db)
	ctx := context.Background()

	details := newProject(t, db, svc, "cascade-"+uuid.NewString())
	userID := uuid.New()
	if err := repo.SetFavorite(ctx, userID, details.ID, true); err != nil {
		t.Fatalf("favourite: %v", err)
	}

	if _, err := svc.DeleteProject(ctx, details.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Unscoped counts, so a soft delete would fail this.
	var secrets, favorites int64
	db.Unscoped().Model(&dbadapter.ProjectSecretModel{}).Where("project_id = ?", details.ID).Count(&secrets)
	db.Unscoped().Model(&dbadapter.ProjectFavoriteModel{}).Where("project_id = ?", details.ID).Count(&favorites)
	if secrets != 0 {
		t.Errorf("secret rows left behind: %d", secrets)
	}
	if favorites != 0 {
		t.Errorf("favourite rows left behind: %d", favorites)
	}
}

func TestDeleteMissingProjectIsNotFound(t *testing.T) {
	db := authTestDB(t)
	svc := projectSvc(t, db)

	status, err := svc.DeleteProject(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected an error deleting a project that does not exist")
	}
	if status != 404 {
		t.Errorf("status = %d, want 404", status)
	}
}

func TestRenameKeepsItsOwnName(t *testing.T) {
	db := authTestDB(t)
	svc := projectSvc(t, db)
	ctx := context.Background()

	name := "self-" + uuid.NewString()
	details := newProject(t, db, svc, name)

	// Saving an unchanged name must not collide with the project itself. This
	// exercises the exclude-self branch, which was unreachable before rename.
	updated, _, err := svc.UpdateProject(ctx, details.ID, &domain.UpdateProjectOpts{
		Name: name, Description: "edited",
	})
	if err != nil {
		t.Fatalf("rename to its own name: %v", err)
	}
	if updated.Description != "edited" {
		t.Errorf("description = %q, want %q", updated.Description, "edited")
	}
}

func TestRenameRejectsAnotherProjectsName(t *testing.T) {
	db := authTestDB(t)
	svc := projectSvc(t, db)
	ctx := context.Background()

	taken := newProject(t, db, svc, "taken-"+uuid.NewString())
	mine := newProject(t, db, svc, "mine-"+uuid.NewString())

	_, status, err := svc.UpdateProject(ctx, mine.ID, &domain.UpdateProjectOpts{Name: taken.Name})
	if err == nil {
		t.Fatal("expected a duplicate name to be rejected")
	}
	if status != 400 {
		t.Errorf("status = %d, want 400", status)
	}
	assertFieldError(t, err, "name")
}

func TestUpdateProjectRejectsEmptyName(t *testing.T) {
	db := authTestDB(t)
	svc := projectSvc(t, db)
	ctx := context.Background()

	details := newProject(t, db, svc, "empty-"+uuid.NewString())

	_, _, err := svc.UpdateProject(ctx, details.ID, &domain.UpdateProjectOpts{Name: "   "})
	if err == nil {
		t.Fatal("expected a whitespace-only name to be rejected")
	}
	assertFieldError(t, err, "name")
}

func TestUpdateProjectClearsDescription(t *testing.T) {
	db := authTestDB(t)
	svc := projectSvc(t, db)
	ctx := context.Background()

	details := newProject(t, db, svc, "clear-"+uuid.NewString())

	// GORM's struct-form Updates skips zero values, so a struct update could
	// never blank a description. This pins the map form.
	updated, _, err := svc.UpdateProject(ctx, details.ID, &domain.UpdateProjectOpts{
		Name: details.Name, Description: "",
	})
	if err != nil {
		t.Fatalf("clear description: %v", err)
	}
	if updated.Description != "" {
		t.Fatalf("in-memory description = %q, want empty", updated.Description)
	}

	reloaded, _, err := svc.GetProject(ctx, details.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Description != "" {
		t.Fatalf("persisted description = %q, want empty", reloaded.Description)
	}
}

// A new project records everything. Sampling is something you reach for when
// volume demands it, and a project that silently started at anything less than
// 100% would look broken to the person who just set it up.
func TestNewProjectRecordsEverySession(t *testing.T) {
	db := authTestDB(t)
	svc := projectSvc(t, db)
	ctx := context.Background()

	details := newProject(t, db, svc, "sample-default-"+uuid.NewString())

	project, _, err := svc.GetProject(ctx, details.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if project.SessionSampleRate != domain.FullSampleRate {
		t.Errorf("a new project samples at %d%%, want %d%%", project.SessionSampleRate, domain.FullSampleRate)
	}
	if project.SampleFraction() != 1 {
		t.Errorf("fraction = %v, want 1", project.SampleFraction())
	}
}

// Zero is the trap. It is a real setting — record nothing — and it is also the
// zero value, so anything that treats "unset" and "off" as the same thing
// cannot store it. The rate must survive a round trip through the database.
func TestUpdateProjectStoresSamplingIncludingZero(t *testing.T) {
	db := authTestDB(t)
	svc := projectSvc(t, db)
	ctx := context.Background()

	details := newProject(t, db, svc, "sample-store-"+uuid.NewString())

	for _, want := range []int{5, 0, 100} {
		rate := want
		if _, _, err := svc.UpdateProject(ctx, details.ID, &domain.UpdateProjectOpts{
			Name: details.Name, SessionSampleRate: &rate,
		}); err != nil {
			t.Fatalf("set sampling to %d: %v", want, err)
		}

		reloaded, _, err := svc.GetProject(ctx, details.ID)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		if reloaded.SessionSampleRate != want {
			t.Fatalf("persisted rate = %d, want %d", reloaded.SessionSampleRate, want)
		}
	}
}

// Renaming a project must not disturb its sampling. nil means "leave it", and
// this is the test that keeps it meaning that.
func TestUpdateProjectLeavesSamplingAloneWhenUnset(t *testing.T) {
	db := authTestDB(t)
	svc := projectSvc(t, db)
	ctx := context.Background()

	details := newProject(t, db, svc, "sample-keep-"+uuid.NewString())

	rate := 7
	if _, _, err := svc.UpdateProject(ctx, details.ID, &domain.UpdateProjectOpts{
		Name: details.Name, SessionSampleRate: &rate,
	}); err != nil {
		t.Fatalf("set sampling: %v", err)
	}

	// A rename with nothing to say about sampling.
	renamed := "renamed-" + uuid.NewString()
	if _, _, err := svc.UpdateProject(ctx, details.ID, &domain.UpdateProjectOpts{Name: renamed}); err != nil {
		t.Fatalf("rename: %v", err)
	}

	reloaded, _, err := svc.GetProject(ctx, details.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.SessionSampleRate != 7 {
		t.Errorf("a rename changed sampling to %d%%, want it left at 7%%", reloaded.SessionSampleRate)
	}
}

func TestUpdateProjectRejectsSamplingOutOfRange(t *testing.T) {
	db := authTestDB(t)
	svc := projectSvc(t, db)
	ctx := context.Background()

	details := newProject(t, db, svc, "sample-range-"+uuid.NewString())

	for _, bad := range []int{-1, 101} {
		rate := bad
		_, status, err := svc.UpdateProject(ctx, details.ID, &domain.UpdateProjectOpts{
			Name: details.Name, SessionSampleRate: &rate,
		})
		if err == nil {
			t.Fatalf("%d%% should be rejected", bad)
		}
		if status != 400 {
			t.Errorf("status = %d, want 400", status)
		}
		assertFieldError(t, err, "session_sample_rate")
	}

	// And the rejection left the stored value alone.
	reloaded, _, err := svc.GetProject(ctx, details.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.SessionSampleRate != domain.FullSampleRate {
		t.Errorf("a rejected save changed the rate to %d%%", reloaded.SessionSampleRate)
	}
}

func TestRevealSecretMatchesTheCreatedSecret(t *testing.T) {
	db := authTestDB(t)
	svc := projectSvc(t, db)
	ctx := context.Background()

	details := newProject(t, db, svc, "reveal-"+uuid.NewString())

	revealed, _, err := svc.RevealSecret(ctx, details.ID)
	if err != nil {
		t.Fatalf("reveal: %v", err)
	}
	if revealed != details.SecretKey {
		t.Fatal("revealed secret does not match the one shown at creation")
	}
}
