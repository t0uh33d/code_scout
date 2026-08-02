package main

import (
	"context"
	"os"
	"time"

	confs "github.com/t0uh33d/code_scout/conf"
	dbadapter "github.com/t0uh33d/code_scout/internal/adapters/db"
	"github.com/t0uh33d/code_scout/internal/services"
	"github.com/t0uh33d/code_scout/jobs"
	"github.com/t0uh33d/code_scout/pkg/cslog"
	"github.com/t0uh33d/code_scout/pkg/sse"
	"github.com/t0uh33d/code_scout/server"
	"github.com/t0uh33d/code_scout/server/handlers"
)

var BuildTime = "-"
var BranchName = "-"
var CommitHash = "-"
var DirtyFiles = "-"

func main() {
	// Subcommands run and exit instead of starting the server. The only one so
	// far is reset-password: no admin outranks the super admin and no email is
	// ever sent, so the recovery path for a lost password is shell access to
	// the server itself.
	if len(os.Args) > 1 && os.Args[1] == "reset-password" {
		os.Exit(runResetPassword(os.Args[2:]))
	}

	log := cslog.GetLogger().WithField("component", "startup")
	ctx := cslog.WithLogger(context.Background(), log)

	log.Info("Starting Code Scout...")
	log.WithFields(map[string]any{
		"build_time": BuildTime,
		"branch":     BranchName,
		"commit":     CommitHash,
	}).Info("Build info")

	if err := confs.Load(); err != nil {
		log.WithError(err).Fatal("Invalid configuration")
	}
	log.Info("Config: " + confs.Conf.Redacted())

	// Initialize database connection
	db, err := dbadapter.NewConnection(dbadapter.DBConfig{
		User:            confs.Conf.DBUser,
		Password:        confs.Conf.DBPassword,
		Database:        confs.Conf.DBName,
		Host:            confs.Conf.DBHost,
		Port:            confs.Conf.DBPort,
		SSLMode:         confs.Conf.DBSSLMode,
		MaxOpenConns:    confs.Conf.MaxOpenConns,
		MaxIdleConns:    confs.Conf.MaxIdleConns,
		ConnMaxLifetime: time.Duration(confs.Conf.ConnMaxLifetime) * time.Minute,
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
	userRepo := dbadapter.NewUserRepo(db)
	memberRepo := dbadapter.NewMemberRepo(db)

	// Create transaction manager
	txMgr := dbadapter.NewTransactionManager(db)

	// Create SSE broker for real-time log streaming
	sseBroker := sse.NewBroker()

	// Create services
	projectSvc := services.NewProjectService(projectRepo, memberRepo, txMgr)
	logSvc := services.NewLogService(logRepo, txMgr, sseBroker)
	authSvc := services.NewAuthService(userRepo)
	memberSvc := services.NewMemberService(userRepo, memberRepo, txMgr)
	logQuerySvc := services.NewLogQueryService(logRepo)
	retentionSvc := services.NewRetentionService(logRepo, 30, 7)

	// Create handlers
	projectHandler := handlers.NewProjectHandler(projectSvc, memberSvc)
	logHandler := handlers.NewLogHandler(logSvc)
	viewHandler := handlers.NewViewHandler(authSvc, projectSvc)
	authHandler := handlers.NewAuthHandler(authSvc)
	dashboardHandler := handlers.NewDashboardHandler(projectSvc)
	logViewerHandler := handlers.NewLogViewerHandler(logQuerySvc, projectSvc, sseBroker)
	projectSettingsHandler := handlers.NewProjectSettingsHandler(projectSvc, memberSvc)
	memberHandler := handlers.NewMemberHandler(memberSvc, projectSvc)
	exportHandler := handlers.NewExportHandler(logQuerySvc)

	// Start cron scheduler
	go jobs.StartScheduler(ctx, retentionSvc)

	// Create and run server
	srv := server.New(server.ServerOpts{
		Host:                   confs.Conf.ServerHost,
		Port:                   confs.Conf.ServerPort,
		DB:                     db,
		Commit:                 CommitHash,
		ProjectSvc:             projectSvc,
		AuthSvc:                authSvc,
		MemberSvc:              memberSvc,
		ProjectHandler:         projectHandler,
		LogHandler:             logHandler,
		ViewHandler:            viewHandler,
		AuthHandler:            authHandler,
		DashboardHandler:       dashboardHandler,
		LogViewerHandler:       logViewerHandler,
		ProjectSettingsHandler: projectSettingsHandler,
		MemberHandler:          memberHandler,
		ExportHandler:          exportHandler,
	})

	go srv.Run()

	if err := srv.HandleGracefulShutdown(); err != nil {
		log.WithError(err).Error("Shutdown error")
		os.Exit(1)
	}

	// Close SSE broker on shutdown
	sseBroker.Close()
}
