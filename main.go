package main

import (
	"context"
	"os"
	"strconv"

	confs "github.com/t0uh33d/code_scout/conf"
	dbadapter "github.com/t0uh33d/code_scout/internal/adapters/db"
	"github.com/t0uh33d/code_scout/internal/services"
	"github.com/t0uh33d/code_scout/jobs"
	"github.com/t0uh33d/code_scout/pkg/cslog"
	"github.com/t0uh33d/code_scout/server"
	"github.com/t0uh33d/code_scout/server/handlers"
)

var BuildTime = "-"
var BranchName = "-"
var CommitHash = "-"
var DirtyFiles = "-"

func main() {
	log := cslog.GetLogger().WithField("component", "startup")
	ctx := cslog.WithLogger(context.Background(), log)

	log.Info("Starting Code Scout...")
	log.WithField("build_time", BuildTime).Info("Build info")
	log.WithField("branch", BranchName).Info("Branch info")
	log.WithField("commit", CommitHash).Info("Commit info")
	log.WithField("dirty_files", DirtyFiles).Info("Dirty files")

	// Initialize database connection
	db, err := dbadapter.NewConnection(dbadapter.DBConfig{
		User:     confs.Conf.MySQLUser,
		Password: confs.Conf.MySQLPassword,
		Database: confs.Conf.MySQLDatabase,
		Host:     confs.Conf.MySQLHost,
		Port:     confs.Conf.MySQLPort,
	})
	if err != nil {
		log.WithError(err).Fatal("Failed to connect to database")
	}

	// Run migrations
	if err := dbadapter.AutoMigrate(db); err != nil {
		log.WithError(err).Fatal("Failed to migrate database")
	}

	// Create repositories
	projectRepo := dbadapter.NewProjectRepo(db)
	logRepo := dbadapter.NewLogRepo(db)

	// Create services
	projectSvc := services.NewProjectService(projectRepo, db)
	logSvc := services.NewLogService(logRepo, db)

	// Create handlers
	projectHandler := handlers.NewProjectHandler(projectSvc)
	logHandler := handlers.NewLogHandler(logSvc)
	viewHandler := handlers.NewViewHandler()

	// Determine address
	port := os.Getenv("PORT")
	if port == "" {
		port = strconv.Itoa(confs.Conf.ServerPort)
	}
	portInt, _ := strconv.Atoi(port)

	host := os.Getenv("HOST")
	if host == "" {
		host = confs.Conf.ServerHost
	}

	// Start cron scheduler
	go jobs.StartScheduler(ctx)

	// Create and run server
	srv := server.New(server.ServerOpts{
		Host:           host,
		Port:           portInt,
		DB:             db,
		ProjectRepo:    projectRepo,
		ProjectHandler: projectHandler,
		LogHandler:     logHandler,
		ViewHandler:    viewHandler,
	})

	go srv.Run()

	if err := srv.HandleGracefulShutdown(); err != nil {
		log.WithError(err).Error("Shutdown error")
		os.Exit(1)
	}
}
