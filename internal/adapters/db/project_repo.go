package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/pkg/cslog"
	"gorm.io/gorm"
)

type ProjectRepo struct {
	db *gorm.DB
}

func NewProjectRepo(db *gorm.DB) *ProjectRepo {
	return &ProjectRepo{db: db}
}

func (r *ProjectRepo) DB() *gorm.DB {
	return r.db
}

func (r *ProjectRepo) GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*domain.Project, error) {
	log := cslog.L(ctx)
	log.WithField("id", id).Debug("DB: GetProjectByID")

	model := &ProjectModel{}
	err := tx.WithContext(ctx).Where("id = ?", id).First(model).Error
	if err != nil {
		log.WithError(err).Error("DB: GetProjectByID failed")
		return nil, err
	}
	return ProjectModelToDomain(model), nil
}

func (r *ProjectRepo) GetByName(ctx context.Context, tx *gorm.DB, name string) (*domain.Project, error) {
	log := cslog.L(ctx)
	log.WithField("name", name).Debug("DB: GetProjectByName")

	model := &ProjectModel{}
	err := tx.WithContext(ctx).Where("name = ?", name).First(model).Error
	if err != nil {
		return nil, err
	}
	return ProjectModelToDomain(model), nil
}

func (r *ProjectRepo) Create(ctx context.Context, tx *gorm.DB, project *domain.Project) error {
	log := cslog.L(ctx)
	log.WithField("name", project.Name).Debug("DB: CreateProject")

	model := ProjectDomainToModel(project)
	if err := model.Create(tx); err != nil {
		log.WithError(err).Error("DB: CreateProject failed")
		return err
	}
	project.ID = model.ID
	project.CreatedAt = model.CreatedAt
	project.UpdatedAt = model.UpdatedAt
	return nil
}

func (r *ProjectRepo) Delete(ctx context.Context, tx *gorm.DB, project *domain.Project) error {
	log := cslog.L(ctx)
	log.WithField("id", project.ID).Debug("DB: DeleteProject")

	model := ProjectDomainToModel(project)
	return model.Delete(tx)
}

func (r *ProjectRepo) GetSecret(ctx context.Context, tx *gorm.DB, projectID uuid.UUID) (*domain.ProjectSecret, error) {
	log := cslog.L(ctx)
	log.WithField("project_id", projectID).Debug("DB: GetProjectSecret")

	model := &ProjectSecretModel{}
	err := tx.WithContext(ctx).Where("project_id = ?", projectID).First(model).Error
	if err != nil {
		return nil, err
	}
	return ProjectSecretModelToDomain(model), nil
}

func (r *ProjectRepo) CreateSecret(ctx context.Context, tx *gorm.DB, secret *domain.ProjectSecret) error {
	log := cslog.L(ctx)
	log.Debug("DB: CreateProjectSecret")

	model := ProjectSecretDomainToModel(secret)
	if err := model.Create(tx); err != nil {
		log.WithError(err).Error("DB: CreateProjectSecret failed")
		return err
	}
	secret.ID = model.ID
	return nil
}

func (r *ProjectRepo) DeleteSecret(ctx context.Context, tx *gorm.DB, secret *domain.ProjectSecret) error {
	log := cslog.L(ctx)
	log.WithField("id", secret.ID).Debug("DB: DeleteProjectSecret")

	model := ProjectSecretDomainToModel(secret)
	return model.Delete(tx)
}
