package db

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ProjectRepo struct {
	db *gorm.DB
}

func NewProjectRepo(db *gorm.DB) *ProjectRepo {
	return &ProjectRepo{db: db}
}

// notFound translates GORM's sentinel into the domain's, so services can branch
// on "missing" without importing gorm.
func notFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ErrNotFound
	}
	return err
}

func (r *ProjectRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
	log := cslog.L(ctx)
	log.WithField("id", id).Debug("DB: GetProjectByID")

	db := getDB(ctx, r.db)
	model := &ProjectModel{}
	err := db.WithContext(ctx).Where("id = ?", id).First(model).Error
	if err != nil {
		log.WithError(err).Error("DB: GetProjectByID failed")
		return nil, notFound(err)
	}
	return ProjectModelToDomain(model), nil
}

func (r *ProjectRepo) GetByName(ctx context.Context, name string) (*domain.Project, error) {
	log := cslog.L(ctx)
	log.WithField("name", name).Debug("DB: GetProjectByName")

	db := getDB(ctx, r.db)
	model := &ProjectModel{}
	err := db.WithContext(ctx).Where("name = ?", name).First(model).Error
	if err != nil {
		return nil, notFound(err)
	}
	return ProjectModelToDomain(model), nil
}

// Update persists Name and Description for an existing project. It never
// creates, and GORM's soft-delete scope keeps it from touching a deleted row.
func (r *ProjectRepo) Update(ctx context.Context, project *domain.Project) error {
	log := cslog.L(ctx)
	log.WithField("id", project.ID).Debug("DB: UpdateProject")

	db := getDB(ctx, r.db)
	// The map form, not a struct: GORM skips zero-valued struct fields, so a
	// struct update could never clear a description.
	res := db.WithContext(ctx).Model(&ProjectModel{}).
		Where("id = ?", project.ID).
		Updates(map[string]any{
			"name":                project.Name,
			"description":         project.Description,
			"session_sample_rate": project.SessionSampleRate,
		})
	if res.Error != nil {
		log.WithError(res.Error).Error("DB: UpdateProject failed")
		return res.Error
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// LockProject reads a project taking a row-level write lock. Only meaningful
// inside a transaction, where it serialises concurrent rename, rotate and
// delete on the same project.
func (r *ProjectRepo) LockProject(ctx context.Context, id uuid.UUID) (*domain.Project, error) {
	db := getDB(ctx, r.db)
	model := &ProjectModel{}
	err := db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", id).First(model).Error
	if err != nil {
		return nil, notFound(err)
	}
	return ProjectModelToDomain(model), nil
}

// DeleteSecretsByProject hard-deletes every secret row for a project, live or
// soft-deleted. Zero rows is success, not an error.
func (r *ProjectRepo) DeleteSecretsByProject(ctx context.Context, projectID uuid.UUID) (int64, error) {
	db := getDB(ctx, r.db)
	// Unscoped, because a plaintext credential that is no longer valid has no
	// business staying in the table, and because secret_key is a unique index
	// that a tombstone would occupy forever.
	res := db.WithContext(ctx).Unscoped().
		Where("project_id = ?", projectID).
		Delete(&ProjectSecretModel{})
	return res.RowsAffected, res.Error
}

// ReplaceSecret removes every secret row for the project and inserts exactly
// one. Deleting all of them first makes rotation self-healing: it produces one
// live row whether the project previously had none, one, or several.
func (r *ProjectRepo) ReplaceSecret(ctx context.Context, projectID uuid.UUID, secretKey string) (*domain.ProjectSecret, error) {
	log := cslog.L(ctx)
	log.WithField("project_id", projectID).Debug("DB: ReplaceSecret")

	if _, err := r.DeleteSecretsByProject(ctx, projectID); err != nil {
		log.WithError(err).Error("DB: ReplaceSecret delete failed")
		return nil, err
	}

	db := getDB(ctx, r.db)
	model := &ProjectSecretModel{ProjectID: projectID.String(), SecretKey: secretKey}
	if err := db.WithContext(ctx).Create(model).Error; err != nil {
		log.WithError(err).Error("DB: ReplaceSecret insert failed")
		return nil, err
	}
	return ProjectSecretModelToDomain(model), nil
}

// DeleteFavoritesByProject hard-deletes every favourite pointing at the
// project, across all users. Matches SetFavorite, which also hard-deletes.
func (r *ProjectRepo) DeleteFavoritesByProject(ctx context.Context, projectID uuid.UUID) (int64, error) {
	db := getDB(ctx, r.db)
	res := db.WithContext(ctx).Unscoped().
		Where("project_id = ?", projectID).
		Delete(&ProjectFavoriteModel{})
	return res.RowsAffected, res.Error
}

func (r *ProjectRepo) Create(ctx context.Context, project *domain.Project) error {
	log := cslog.L(ctx)
	log.WithField("name", project.Name).Debug("DB: CreateProject")

	db := getDB(ctx, r.db)
	model := ProjectDomainToModel(project)
	if err := model.Create(db); err != nil {
		log.WithError(err).Error("DB: CreateProject failed")
		return err
	}
	project.ID = model.ID
	project.CreatedAt = model.CreatedAt
	project.UpdatedAt = model.UpdatedAt
	return nil
}

func (r *ProjectRepo) Delete(ctx context.Context, project *domain.Project) error {
	log := cslog.L(ctx)
	log.WithField("id", project.ID).Debug("DB: DeleteProject")

	db := getDB(ctx, r.db)
	model := ProjectDomainToModel(project)
	return model.Delete(db)
}

func (r *ProjectRepo) GetSecret(ctx context.Context, projectID uuid.UUID) (*domain.ProjectSecret, error) {
	log := cslog.L(ctx)
	log.WithField("project_id", projectID).Debug("DB: GetProjectSecret")

	db := getDB(ctx, r.db)
	model := &ProjectSecretModel{}
	// First orders by primary key, and keys are UUIDv7 (time-ordered), so this
	// returns the OLDEST live row. Rotation therefore has to remove every row
	// rather than adding one, or auth would keep using the superseded key.
	err := db.WithContext(ctx).Where("project_id = ?", projectID).First(model).Error
	if err != nil {
		return nil, notFound(err)
	}
	return ProjectSecretModelToDomain(model), nil
}

func (r *ProjectRepo) CreateSecret(ctx context.Context, secret *domain.ProjectSecret) error {
	log := cslog.L(ctx)
	log.Debug("DB: CreateProjectSecret")

	db := getDB(ctx, r.db)
	model := ProjectSecretDomainToModel(secret)
	if err := model.Create(db); err != nil {
		log.WithError(err).Error("DB: CreateProjectSecret failed")
		return err
	}
	secret.ID = model.ID
	return nil
}

func (r *ProjectRepo) DeleteSecret(ctx context.Context, secret *domain.ProjectSecret) error {
	log := cslog.L(ctx)
	log.WithField("id", secret.ID).Debug("DB: DeleteProjectSecret")

	db := getDB(ctx, r.db)
	model := ProjectSecretDomainToModel(secret)
	return model.Delete(db)
}

// SetFavorite adds or removes a favourite and reports the resulting state.
// Adding is idempotent: clicking a star twice in quick succession must not fail
// on the unique index.
func (r *ProjectRepo) SetFavorite(ctx context.Context, userID, projectID uuid.UUID, favorite bool) error {
	log := cslog.L(ctx)
	db := getDB(ctx, r.db)

	if !favorite {
		// Unscoped, so this is a real delete. A soft delete would leave the row
		// matching the raw join in List (which does not apply GORM's soft-delete
		// scope) and would also collide with the unique index on re-starring.
		// Un-starring is not an event worth keeping.
		err := db.WithContext(ctx).Unscoped().
			Where("user_id = ? AND project_id = ?", userID, projectID).
			Delete(&ProjectFavoriteModel{}).Error
		if err != nil {
			log.WithError(err).Error("DB: RemoveFavorite failed")
		}
		return err
	}

	model := &ProjectFavoriteModel{UserID: userID, ProjectID: projectID}
	err := db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "project_id"}},
			DoNothing: true,
		}).
		Create(model).Error
	if err != nil {
		log.WithError(err).Error("DB: AddFavorite failed")
	}
	return err
}

// IsFavorite reports whether the user has starred this project.
func (r *ProjectRepo) IsFavorite(ctx context.Context, userID, projectID uuid.UUID) (bool, error) {
	db := getDB(ctx, r.db)
	var count int64
	err := db.WithContext(ctx).Model(&ProjectFavoriteModel{}).
		Where("user_id = ? AND project_id = ?", userID, projectID).
		Count(&count).Error
	return count > 0, err
}

func (r *ProjectRepo) List(ctx context.Context, opts domain.ProjectListOpts) (*domain.ProjectListResult, error) {
	log := cslog.L(ctx)
	log.Debug("DB: ListProjects")

	db := getDB(ctx, r.db)

	if opts.Page < 1 {
		opts.Page = 1
	}
	if opts.PageSize < 1 {
		opts.PageSize = 12
	}

	query := db.WithContext(ctx).Model(&ProjectModel{})

	if opts.Search != "" {
		query = query.Where(`projects.name ILIKE ? ESCAPE '\'`, containsPattern(opts.Search))
	}

	// One join carries both jobs: an inner join filters to the user's
	// favourites, a left join just lights up the star on the ones that are.
	// The SELECT below must only mention project_favorites when the join is
	// present, or a caller without a user would get invalid SQL.
	selectClause := "projects.*, FALSE AS is_favorite"
	if opts.UserID != uuid.Nil {
		join := "LEFT JOIN project_favorites ON project_favorites.project_id = projects.id AND project_favorites.user_id = ?"
		if opts.FavoritesOnly {
			join = "JOIN project_favorites ON project_favorites.project_id = projects.id AND project_favorites.user_id = ?"
		}
		query = query.Joins(join, opts.UserID)
		selectClause = "projects.*, project_favorites.id IS NOT NULL AS is_favorite"
	} else if opts.FavoritesOnly {
		// No user means no favourites, rather than every project.
		return &domain.ProjectListResult{Items: []domain.ProjectListItem{}, Page: opts.Page, PageSize: opts.PageSize}, nil
	}

	// Visibility. Everyone except a super admin sees exactly the projects they
	// have a membership row for. An inner join is what enforces it, so a
	// project the caller is not on cannot appear in the list, the count, or the
	// pagination totals.
	if opts.RestrictToMemberships {
		if opts.UserID == uuid.Nil {
			// Restricted with nobody to restrict to means nothing is visible.
			// Failing closed here matters: the alternative is leaking the whole
			// instance to an unauthenticated caller.
			return &domain.ProjectListResult{Items: []domain.ProjectListItem{}, Page: opts.Page, PageSize: opts.PageSize}, nil
		}
		query = query.Joins(
			"JOIN project_members ON project_members.project_id = projects.id AND project_members.user_id = ?",
			opts.UserID)
	}

	var totalCount int64
	if err := query.Count(&totalCount).Error; err != nil {
		log.WithError(err).Error("DB: ListProjects count failed")
		return nil, err
	}

	// projectRow is ProjectModel plus the joined favourite flag. Selecting it in
	// the same statement avoids a follow-up query per project.
	type projectRow struct {
		ProjectModel
		IsFavorite bool
	}

	offset := (opts.Page - 1) * opts.PageSize
	var models []projectRow
	if err := query.
		Select(selectClause).
		Order("projects.created_at DESC").
		Offset(offset).Limit(opts.PageSize).
		Find(&models).Error; err != nil {
		log.WithError(err).Error("DB: ListProjects query failed")
		return nil, err
	}

	items := make([]domain.ProjectListItem, 0, len(models))
	for _, m := range models {
		items = append(items, domain.ProjectListItem{
			ID:          m.ID,
			Name:        m.Name,
			Description: m.Description,
			IsFavorite:  m.IsFavorite,
			CreatedAt:   m.CreatedAt,
		})
	}

	totalPages := int(totalCount) / opts.PageSize
	if int(totalCount)%opts.PageSize > 0 {
		totalPages++
	}

	return &domain.ProjectListResult{
		Items:      items,
		TotalCount: totalCount,
		Page:       opts.Page,
		PageSize:   opts.PageSize,
		TotalPages: totalPages,
	}, nil
}
