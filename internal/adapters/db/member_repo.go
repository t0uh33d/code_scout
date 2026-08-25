package db

import (
	"context"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MemberRepo owns project membership: who is on which project and at what
// level. Accounts themselves live in UserRepo.
type MemberRepo struct {
	db *gorm.DB
}

func NewMemberRepo(db *gorm.DB) *MemberRepo {
	return &MemberRepo{db: db}
}

// GetMembership returns one user's row for one project, or ErrNotFound. This is
// the call every project-scoped request makes, so it stays a single indexed
// lookup on (user_id, project_id).
func (r *MemberRepo) GetMembership(ctx context.Context, userID, projectID uuid.UUID) (*domain.ProjectMember, error) {
	db := getDB(ctx, r.db)
	model := &ProjectMemberModel{}
	err := db.WithContext(ctx).
		Where("user_id = ? AND project_id = ?", userID, projectID).
		First(model).Error
	if err != nil {
		return nil, notFound(err)
	}
	return ProjectMemberModelToDomain(model), nil
}

// SetMembership adds or updates someone's level on a project. Idempotent, so
// re-adding an existing member changes their level rather than failing.
func (r *MemberRepo) SetMembership(ctx context.Context, userID, projectID uuid.UUID, level domain.ProjectLevel) error {
	log := cslog.L(ctx)
	db := getDB(ctx, r.db)

	model := &ProjectMemberModel{UserID: userID, ProjectID: projectID, Level: string(level)}
	err := db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "project_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"level", "updated_at"}),
		}).
		Create(model).Error
	if err != nil {
		log.WithError(err).Error("DB: SetMembership failed")
	}
	return err
}

// RemoveMembership revokes access. Hard delete, matching favourites: a
// tombstone would still satisfy the unique index and silently block re-adding
// the person later.
func (r *MemberRepo) RemoveMembership(ctx context.Context, userID, projectID uuid.UUID) error {
	db := getDB(ctx, r.db)
	return db.WithContext(ctx).Unscoped().
		Where("user_id = ? AND project_id = ?", userID, projectID).
		Delete(&ProjectMemberModel{}).Error
}

// ListProjectMembers returns everyone explicitly on a project, with their
// account details joined in for display. Super admins are not included: their
// access does not come from a row here, so the caller adds them separately.
func (r *MemberRepo) ListProjectMembers(ctx context.Context, projectID uuid.UUID) ([]domain.ProjectMember, error) {
	log := cslog.L(ctx)
	db := getDB(ctx, r.db)

	type row struct {
		ProjectMemberModel
		Name  string
		Email string
		Role  string
	}

	var rows []row
	err := db.WithContext(ctx).
		Model(&ProjectMemberModel{}).
		Select("project_members.*, users.name, users.email, users.role").
		Joins("JOIN users ON users.id = project_members.user_id AND users.deleted_at IS NULL").
		Where("project_members.project_id = ?", projectID).
		Order("users.name ASC").
		Find(&rows).Error
	if err != nil {
		log.WithError(err).Error("DB: ListProjectMembers failed")
		return nil, err
	}

	members := make([]domain.ProjectMember, 0, len(rows))
	for _, r := range rows {
		members = append(members, domain.ProjectMember{
			ID:        r.ID,
			UserID:    r.UserID,
			ProjectID: r.ProjectID,
			Level:     domain.ProjectLevel(r.Level),
			CreatedAt: r.CreatedAt,
			Name:      r.Name,
			Email:     r.Email,
			Role:      domain.Role(r.Role),
		})
	}
	return members, nil
}

// ListUserProjectIDs returns the projects one account belongs to. Used to show
// "which projects can this person see" on the Members screen.
func (r *MemberRepo) ListUserProjectIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	db := getDB(ctx, r.db)
	var ids []uuid.UUID
	err := db.WithContext(ctx).
		Model(&ProjectMemberModel{}).
		Joins("JOIN projects ON projects.id = project_members.project_id AND projects.deleted_at IS NULL").
		Where("project_members.user_id = ?", userID).
		Pluck("project_members.project_id", &ids).Error
	return ids, err
}

// RemoveAllForUser strips every project from an account, used when deleting it.
func (r *MemberRepo) RemoveAllForUser(ctx context.Context, userID uuid.UUID) error {
	db := getDB(ctx, r.db)
	return db.WithContext(ctx).Unscoped().
		Where("user_id = ?", userID).
		Delete(&ProjectMemberModel{}).Error
}

// RemoveAllForProject strips every member from a project, used when deleting it.
func (r *MemberRepo) RemoveAllForProject(ctx context.Context, projectID uuid.UUID) error {
	db := getDB(ctx, r.db)
	return db.WithContext(ctx).Unscoped().
		Where("project_id = ?", projectID).
		Delete(&ProjectMemberModel{}).Error
}
