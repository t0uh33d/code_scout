package db

import (
	"fmt"

	"github.com/t0uh33d/code_scout/pkg/cslog"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type DBConfig struct {
	User     string
	Password string
	Database string
	Host     string
	Port     int
}

func NewConnection(cfg DBConfig) (*gorm.DB, error) {
	dbURI := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local&sql_mode=''",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.Database,
	)
	cslog.Info(dbURI)

	db, err := gorm.Open(mysql.Open(dbURI), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	return db, nil
}

func AutoMigrate(db *gorm.DB) error {
	return db.Debug().AutoMigrate(
		&ProjectModel{},
		&ProjectSecretModel{},
		&LogModel{},
	)
}
