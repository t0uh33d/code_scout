package confs

import (
	"log"

	"github.com/pelletier/go-toml"
)

type Configuration struct {
	ServerHost string
	ServerPort int

	MySQLUser     string
	MySQLPassword string
	MySQLDatabase string
	MySQLHost     string
	MySQLPort     int
}

var Conf Configuration

func init() {

	confFile := "/etc/code-scout.conf"

	config, err := toml.LoadFile(confFile)
	if err != nil {
		log.Println(err)
	}
	var ok bool

	port, ok := config.Get("port").(int64)
	if ok {
		Conf.ServerPort = int(port)
	} else {
		Conf.ServerPort = 24275
	}

	host, ok := config.Get("host").(string)
	if ok {
		Conf.ServerHost = host
	} else {
		Conf.ServerHost = "localhost"
	}

	mySQLDatabase, ok := config.Get("mysql_database").(string)
	if ok {
		Conf.MySQLDatabase = mySQLDatabase
	} else {
		Conf.MySQLDatabase = "main_db"
	}

	mySQLUser, ok := config.Get("mysql_user").(string)
	if ok {
		Conf.MySQLUser = mySQLUser
	} else {
		Conf.MySQLUser = "root"
	}

	mySQLPassword, ok := config.Get("mysql_password").(string)
	if ok {
		Conf.MySQLPassword = mySQLPassword
	} else {
		Conf.MySQLPassword = ""
	}

	mySQLHOST, ok := config.Get("mysql_host").(string)
	if ok {
		Conf.MySQLHost = mySQLHOST
	} else {
		Conf.MySQLHost = "localhost"
	}

	mySQLPort, ok := config.Get("mysql_port").(int64)
	if ok {
		Conf.MySQLPort = int(mySQLPort)
	} else {
		Conf.MySQLPort = 3306
	}

}
