package db

import (
	"fmt"

	confs "github.com/t0uh33d/code_scout/conf"
	"github.com/t0uh33d/code_scout/utils/oalog"

	"gorm.io/driver/mysql"

	"gorm.io/gorm"
)

var (
	GormDB *gorm.DB
)

var AllTables = []interface{}{}

func init() {
	dbURI := fmt.Sprintf("%s:%s@tcp(127.0.0.1:3306)/%s?charset=utf8mb4&parseTime=True&loc=Local&sql_mode=''",
		confs.Conf.MySQLUser,
		confs.Conf.MySQLPassword,
		confs.Conf.MySQLDatabase,
	)
	oalog.Info(dbURI)
	fmt.Println("inside db init ")

	db, err := gorm.Open(mysql.Open(dbURI), &gorm.Config{})
	if err != nil {
		oalog.Error(err)
	}

	db.Debug().AutoMigrate(AllTables...)

	// Migrate tables first
	if err := db.Debug().AutoMigrate(AllTables...); err != nil {
		oalog.Fatal("Error migrating tables: ", err)
	}

	GormDB = db
}
