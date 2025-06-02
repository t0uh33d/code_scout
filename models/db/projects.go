package db

import (
	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/utils"
	"gorm.io/gorm"
)

type Projects struct {
	utils.GormBase

	Name        string `gorm:"type:varchar(255);not null"`
	Description string `gorm:"type:text;not null"`
}

type ProjectSecret struct {
	utils.GormBase
	ProjectID string `gorm:"type:char(36);not null;index"`
	SecretKey string `gorm:"type:varchar(255);not null;uniqueIndex"`

	Project Projects `gorm:"foreignKey:ProjectID;references:ID"`
}

func (gr *Projects) Create(tx *gorm.DB) error {
	return tx.Create(gr).Error
}

func (gr *Projects) Update(tx *gorm.DB) error {
	return tx.Save(gr).Error
}

func (gr *Projects) Delete(tx *gorm.DB) error {
	return tx.Delete(gr).Error
}

func GetProjectByName(tx *gorm.DB, name string) (*Projects, error) {
	project := &Projects{}
	err := tx.Where("name = ?", name).First(project).Error
	if err != nil {
		return nil, err
	}
	return project, nil
}

func (gr *ProjectSecret) Create(tx *gorm.DB) error {
	return tx.Create(gr).Error
}

func (gr *ProjectSecret) Update(tx *gorm.DB) error {
	return tx.Save(gr).Error
}

func (gr *ProjectSecret) Delete(tx *gorm.DB) error {
	return tx.Delete(gr).Error
}

func GetProjectByID(tx *gorm.DB, id uuid.UUID) (*Projects, error) {
	project := &Projects{}
	err := tx.Where("id = ?", id).First(project).Error
	if err != nil {
		return nil, err
	}
	return project, nil
}

func GetProjectSecretByProjectID(tx *gorm.DB, projectID uuid.UUID) (*ProjectSecret, error) {
	secret := &ProjectSecret{}
	err := tx.Where("project_id = ?", projectID).First(secret).Error
	if err != nil {
		return nil, err
	}
	return secret, nil
}
