package db

import (
	"context"

	"github.com/t0uh33d/code_scout/internal/domain"
	"github.com/t0uh33d/code_scout/pkg/cslog"
	"gorm.io/gorm"
)

type LogRepo struct {
	db *gorm.DB
}

func NewLogRepo(db *gorm.DB) *LogRepo {
	return &LogRepo{db: db}
}

func (r *LogRepo) DB() *gorm.DB {
	return r.db
}

func (r *LogRepo) CreateBatch(ctx context.Context, tx *gorm.DB, logs []domain.Log) error {
	log := cslog.L(ctx)
	log.WithField("count", len(logs)).Debug("DB: CreateBatch logs")

	for _, l := range logs {
		model := LogDomainToModel(&l)
		if err := model.Create(tx); err != nil {
			log.WithError(err).Error("DB: CreateBatch failed")
			return err
		}
	}
	return nil
}
