package db

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
	"gorm.io/gorm"
)

// clearFavorites keeps a failed assertion from leaking rows into later runs.
func clearFavorites(t *testing.T, db *gorm.DB, userID uuid.UUID) {
	t.Cleanup(func() {
		db.Unscoped().Where("user_id = ?", userID).Delete(&ProjectFavoriteModel{})
	})
}

// Favourites are the one place a raw SQL join meets GORM's soft deletes, so
// these run against a real Postgres. They skip without CS_TEST_DB.

func TestFavoriteToggleRoundTrip(t *testing.T) {
	db := testDB(t)
	repo := NewProjectRepo(db)
	ctx := context.Background()

	userID := uuid.New()
	clearFavorites(t, db, userID)
	projectID := seedProject(t, db)

	if fav, err := repo.IsFavorite(ctx, userID, projectID); err != nil || fav {
		t.Fatalf("expected not favourite initially, got %v (err %v)", fav, err)
	}

	if err := repo.SetFavorite(ctx, userID, projectID, true); err != nil {
		t.Fatalf("add favourite: %v", err)
	}
	if fav, _ := repo.IsFavorite(ctx, userID, projectID); !fav {
		t.Fatal("expected favourite after adding")
	}

	// The regression this file exists for: a soft delete left the row matching
	// the join in List, so an un-starred project stayed in the favourites tab.
	if err := repo.SetFavorite(ctx, userID, projectID, false); err != nil {
		t.Fatalf("remove favourite: %v", err)
	}
	if fav, _ := repo.IsFavorite(ctx, userID, projectID); fav {
		t.Fatal("expected not favourite after removing")
	}

	result, err := repo.List(ctx, domain.ProjectListOpts{
		Page: 1, PageSize: 50, UserID: userID, FavoritesOnly: true,
	})
	if err != nil {
		t.Fatalf("list favourites: %v", err)
	}
	for _, item := range result.Items {
		if item.ID == projectID {
			t.Fatal("un-starred project still appears in the favourites list")
		}
	}
}

func TestFavoriteAddIsIdempotent(t *testing.T) {
	db := testDB(t)
	repo := NewProjectRepo(db)
	ctx := context.Background()

	userID := uuid.New()
	clearFavorites(t, db, userID)
	projectID := seedProject(t, db)

	// A double click must not trip the unique index.
	for i := range 3 {
		if err := repo.SetFavorite(ctx, userID, projectID, true); err != nil {
			t.Fatalf("add favourite %d: %v", i, err)
		}
	}

	result, err := repo.List(ctx, domain.ProjectListOpts{
		Page: 1, PageSize: 50, UserID: userID, FavoritesOnly: true,
	})
	if err != nil {
		t.Fatalf("list favourites: %v", err)
	}
	seen := 0
	for _, item := range result.Items {
		if item.ID == projectID {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("project appears %d times in favourites, want exactly 1", seen)
	}
}

// A caller without a user (uuid.Nil) must still get valid SQL: the SELECT
// references project_favorites only when the join is present.
func TestListWithoutUserIsValidSQL(t *testing.T) {
	db := testDB(t)
	repo := NewProjectRepo(db)
	projectID := seedProject(t, db)

	result, err := repo.List(context.Background(), domain.ProjectListOpts{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("list without user: %v", err)
	}
	for _, item := range result.Items {
		if item.ID == projectID && item.IsFavorite {
			t.Fatal("no user in scope, nothing can be a favourite")
		}
	}
}

func TestFavoritesAreScopedPerUser(t *testing.T) {
	db := testDB(t)
	repo := NewProjectRepo(db)
	ctx := context.Background()

	alice, bob := uuid.New(), uuid.New()
	clearFavorites(t, db, alice)
	clearFavorites(t, db, bob)
	projectID := seedProject(t, db)

	if err := repo.SetFavorite(ctx, alice, projectID, true); err != nil {
		t.Fatalf("alice favourite: %v", err)
	}

	if fav, _ := repo.IsFavorite(ctx, bob, projectID); fav {
		t.Fatal("alice's favourite leaked to bob")
	}

	bobList, err := repo.List(ctx, domain.ProjectListOpts{
		Page: 1, PageSize: 50, UserID: bob, FavoritesOnly: true,
	})
	if err != nil {
		t.Fatalf("list bob favourites: %v", err)
	}
	for _, item := range bobList.Items {
		if item.ID == projectID {
			t.Fatal("bob sees alice's favourite")
		}
	}

	// And the star flag on the full list is per user too.
	aliceList, err := repo.List(ctx, domain.ProjectListOpts{
		Page: 1, PageSize: 50, UserID: alice,
	})
	if err != nil {
		t.Fatalf("list alice: %v", err)
	}
	for _, item := range aliceList.Items {
		if item.ID == projectID && !item.IsFavorite {
			t.Fatal("alice's own list does not show the project as favourite")
		}
	}
}
