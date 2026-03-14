package db

import (
	"context"

	"gorm.io/gorm"
)

type txContextKey struct{}

// GormTransactionManager implements ports.TransactionManager using GORM.
type GormTransactionManager struct {
	db *gorm.DB
}

func NewTransactionManager(db *gorm.DB) *GormTransactionManager {
	return &GormTransactionManager{db: db}
}

func (tm *GormTransactionManager) WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	tx := tm.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}

	txCtx := context.WithValue(ctx, txContextKey{}, tx)

	if err := fn(txCtx); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit().Error
}

// getDB extracts the transactional *gorm.DB from context, falling back to the repo's own db.
func getDB(ctx context.Context, fallback *gorm.DB) *gorm.DB {
	if tx, ok := ctx.Value(txContextKey{}).(*gorm.DB); ok {
		return tx
	}
	return fallback
}
