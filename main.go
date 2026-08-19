package main

import (
	"context"
	"os"
	"time"

	"github.com/getcodescout/code_scout/app"
	confs "github.com/getcodescout/code_scout/conf"
	dbadapter "github.com/getcodescout/code_scout/internal/adapters/db"
	"github.com/getcodescout/code_scout/internal/live"
	"github.com/getcodescout/code_scout/internal/services"
	"github.com/getcodescout/code_scout/jobs"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"github.com/getcodescout/code_scout/pkg/sse"
	"github.com/getcodescout/code_scout/server"
	"github.com/getcodescout/code_scout/server/handlers"
	"github.com/getcodescout/code_scout/server/mcptools"
	"github.com/getcodescout/code_scout/view"
)

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

	log.Info("Starting CodeScout...")
	log.WithFields(map[string]any{
		"version":    app.Version,
		"commit":     app.Commit,
		"build_time": app.BuildTime,
	}).Info("Build info")

	if err := confs.Load(); err != nil {
		log.WithError(err).Fatal("Invalid configuration")
	}

	// As early as possible after the config exists, so as little as possible is
	// written before the operator's level and destination are in force. The
	// lines above this are startup and configuration failures, which belong on
	// stderr regardless of where the rest goes.
	if err := cslog.Configure(cslog.Options{
		Level:      confs.Conf.LogLevel,
		Format:     confs.Conf.LogFormat,
		File:       confs.Conf.LogFile,
		MaxSizeMB:  confs.Conf.LogMaxSizeMB,
		MaxBackups: confs.Conf.LogMaxBackups,
		MaxAgeDays: confs.Conf.LogMaxAgeDays,
		Compress:   confs.Conf.LogCompress,
	}); err != nil {
		log.WithError(err).Fatal("Invalid logging configuration")
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
	sessionRepo := dbadapter.NewSessionRepo(db)
	tokenRepo := dbadapter.NewTokenRepo(db)

	// Create transaction manager
	txMgr := dbadapter.NewTransactionManager(db)

	// Create SSE broker for real-time log streaming
	sseBroker := sse.NewBroker()

	// Create services
	projectSvc := services.NewProjectService(projectRepo, memberRepo, txMgr)
	usageRepo := dbadapter.NewUsageRepo(db)
	authSvc := services.NewAuthService(userRepo)
	memberSvc := services.NewMemberService(userRepo, memberRepo, tokenRepo, txMgr)
	tokenSvc := services.NewTokenService(tokenRepo, userRepo)
	instanceSettingsSvc := services.NewInstanceSettingsService(dbadapter.NewInstanceSettingsRepo(db))
	// Primed before the server accepts traffic, so the first page already renders
	// in the configured zone. A failure leaves it on UTC rather than refusing to
	// boot: the instance is still usable.
	if err := instanceSettingsSvc.Load(ctx); err != nil {
		log.WithError(err).Warn("Instance settings unavailable, rendering in UTC")
	}
	view.SetTimeZone(instanceSettingsSvc.Current().Location())
	view.SetTwelveHourClock(instanceSettingsSvc.Current().TwelveHour())
	logSvc := services.NewLogService(logRepo, txMgr, sseBroker, sessionRepo, usageRepo, instanceSettingsSvc)
	logQuerySvc := services.NewLogQueryService(logRepo, sessionRepo)
	retentionSvc := services.NewRetentionService(logRepo, usageRepo, sessionRepo, instanceSettingsSvc)
	versionSvc := services.NewVersionService(instanceSettingsSvc)

	// The check publishes into the view the same way the timezone does, so the
	// chrome on every page can read one value without it being threaded through
	// every page's data.
	checkVersion := func(ctx context.Context) {
		view.SetUpdateState(versionSvc.Check(ctx))
	}
	// Once at boot as well as on the schedule, so a fresh instance is not blank
	// about updates until tomorrow morning. In its own goroutine because it
	// talks to the internet, and a slow or blackholed DNS lookup must not sit
	// between here and the server accepting traffic.
	go checkVersion(ctx)

	// Create handlers
	projectHandler := handlers.NewProjectHandler(projectSvc, memberSvc)
	logHandler := handlers.NewLogHandler(logSvc, instanceSettingsSvc)
	viewHandler := handlers.NewViewHandler(authSvc, projectSvc)
	authHandler := handlers.NewAuthHandler(authSvc)
	dashboardHandler := handlers.NewDashboardHandler(projectSvc, memberSvc)
	logViewerHandler := handlers.NewLogViewerHandler(logQuerySvc, projectSvc, sseBroker, instanceSettingsSvc)
	projectSettingsHandler := handlers.NewProjectSettingsHandler(projectSvc, memberSvc)
	memberHandler := handlers.NewMemberHandler(memberSvc, projectSvc)
	instanceSettingsHandler := handlers.NewInstanceSettingsHandler(instanceSettingsSvc, memberSvc, projectSvc, versionSvc)
	accountHandler := handlers.NewAccountHandler(tokenSvc)
	exportHandler := handlers.NewExportHandler(logQuerySvc)

	// Live sessions live here and nowhere else. The hub holds them in memory on
	// this process, so it is created alongside the handlers rather than behind a
	// repository — there is nothing to persist.
	liveHub := live.NewHub()
	liveHandler := handlers.NewLiveHandler(liveHub, projectSvc)

	// The MCP endpoint reads through the same service instances as the
	// dashboard's own screens, so the two can never answer differently.
	mcpHandler := mcptools.NewHTTPHandler(mcptools.Deps{
		Logs:     logQuerySvc,
		Projects: projectSvc,
		Access:   memberSvc,
		Settings: instanceSettingsSvc,
		Live:     liveHub,
	})

	// Start cron scheduler
	go jobs.StartScheduler(ctx, retentionSvc, checkVersion)

	// Create and run server
	srv := server.New(server.ServerOpts{
		Host:                    confs.Conf.ServerHost,
		Port:                    confs.Conf.ServerPort,
		DB:                      db,
		ProjectSvc:              projectSvc,
		AuthSvc:                 authSvc,
		MemberSvc:               memberSvc,
		ProjectHandler:          projectHandler,
		LogHandler:              logHandler,
		ViewHandler:             viewHandler,
		AuthHandler:             authHandler,
		DashboardHandler:        dashboardHandler,
		LogViewerHandler:        logViewerHandler,
		ProjectSettingsHandler:  projectSettingsHandler,
		MemberHandler:           memberHandler,
		InstanceSettingsHandler: instanceSettingsHandler,
		AccountHandler:          accountHandler,
		ExportHandler:           exportHandler,
		LiveHandler:             liveHandler,
		TokenSvc:                tokenSvc,
		MCPHandler:              mcpHandler,
	})

	go srv.Run()

	if err := srv.HandleGracefulShutdown(); err != nil {
		log.WithError(err).Error("Shutdown error")
		os.Exit(1)
	}

	// Close SSE broker on shutdown
	sseBroker.Close()
	// And tell every live watcher the server is going, rather than leaving
	// their connections to time out on their own.
	liveHub.Close()
}
