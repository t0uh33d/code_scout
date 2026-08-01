package server

import (
	"io/fs"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/t0uh33d/code_scout/server/handlers"
	"github.com/t0uh33d/code_scout/server/middleware"
	"github.com/t0uh33d/code_scout/view/static"
)

func (s *Server) registerRoutes(router *mux.Router, opts ServerOpts) {
	// Serve embedded static files (CSS, JS, images)
	staticFS, _ := fs.Sub(static.Files, ".")
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Public routes — no session required. /healthz is deliberately outside the
	// logging middleware so orchestrator probes don't bury real requests.
	router.HandleFunc("/healthz", handlers.NewHealthHandler(opts.DB, opts.Commit).Health).Methods("GET")
	router.HandleFunc("/login", opts.ViewHandler.Login).Methods("GET")

	// Auth API routes — no session required
	authRouter := router.PathPrefix("/api/auth").Subrouter()
	authRouter.Use(middleware.HttpLogger)
	authRouter.HandleFunc("/submit", opts.AuthHandler.Submit).Methods("POST")
	authRouter.HandleFunc("/logout", opts.AuthHandler.Logout).Methods("POST")

	// Protected web page routes — require valid session
	webRouter := router.NewRoute().Subrouter()
	webRouter.Use(middleware.RequireSession(opts.AuthSvc))
	webRouter.HandleFunc("/", opts.ViewHandler.Dashboard).Methods("GET")

	// Dashboard HTMX partial routes — session protected
	webRouter.HandleFunc("/dashboard/projects", opts.DashboardHandler.ProjectsGrid).Methods("GET")
	webRouter.HandleFunc("/dashboard/projects", opts.DashboardHandler.CreateProject).Methods("POST")
	webRouter.HandleFunc("/dashboard/projects/new", opts.DashboardHandler.NewProjectWizard).Methods("GET")
	webRouter.HandleFunc("/dashboard/projects/list", opts.DashboardHandler.ProjectsListPartial).Methods("GET")
	webRouter.HandleFunc("/dashboard/projects/{id}/favorite", opts.DashboardHandler.ToggleFavorite).Methods("POST")

	// Log viewer routes — session protected
	webRouter.HandleFunc("/project/{id}/logs", opts.LogViewerHandler.LogViewer).Methods("GET")
	webRouter.HandleFunc("/project/{id}/logs/partial", opts.LogViewerHandler.LogsPartial).Methods("GET")
	webRouter.HandleFunc("/project/{id}/session/{sid}", opts.LogViewerHandler.SessionTimeline).Methods("GET")
	webRouter.HandleFunc("/project/{id}/network/{rid}", opts.LogViewerHandler.NetworkDetail).Methods("GET")
	webRouter.HandleFunc("/dashboard/projects/{id}/stats", opts.LogViewerHandler.ProjectStats).Methods("GET")

	// Project settings — session protected, browser only. These sit under
	// /project/, so they never collide with the SDK's /api prefix router.
	webRouter.HandleFunc("/project/{id}/settings", opts.ProjectSettingsHandler.Settings).Methods("GET")
	webRouter.HandleFunc("/project/{id}/settings/general", opts.ProjectSettingsHandler.UpdateGeneral).Methods("POST")
	webRouter.HandleFunc("/project/{id}/settings/secret", opts.ProjectSettingsHandler.RevealSecret).Methods("GET")
	webRouter.HandleFunc("/project/{id}/settings/secret/rotate", opts.ProjectSettingsHandler.RotateSecret).Methods("POST")
	webRouter.HandleFunc("/project/{id}/settings/confirm", opts.ProjectSettingsHandler.ConfirmDialog).Methods("GET")
	webRouter.HandleFunc("/project/{id}/settings/delete", opts.ProjectSettingsHandler.DeleteProject).Methods("POST")

	// SSE streaming — session protected
	webRouter.HandleFunc("/stream/logs", opts.LogViewerHandler.StreamLogs).Methods("GET")

	// Export — session protected
	webRouter.HandleFunc("/export/logs", opts.ExportHandler.ExportLogs).Methods("GET")

	// Project management is an operator action, not an SDK action — it
	// requires a web session, never SDK headers. Registered on webRouter
	// (before the /api prefix router) so these two match first.
	webRouter.HandleFunc("/api/project", opts.ProjectHandler.CreateProject).Methods("POST")
	webRouter.HandleFunc("/api/project/{project_id}", opts.ProjectHandler.DeleteProject).Methods("DELETE")

	// SDK API subrouter — every route requires X-Project-ID/X-Project-Secret
	apiRouter := router.PathPrefix("/api").Subrouter()
	apiRouter.Use(middleware.HttpLogger)
	apiRouter.Use(middleware.ConnectionCloseMiddleware)
	apiRouter.Use(middleware.CorsMiddleware)
	apiRouter.Use(middleware.JsonContentTypeMiddleware)
	apiRouter.Use(middleware.Authenticate(s.projectSvc))

	apiRouter.HandleFunc("/validate", opts.ProjectHandler.Validate).Methods("GET")
	apiRouter.HandleFunc("/logs/dump", opts.LogHandler.DumpLogs).Methods("POST")

	// Panic recovery (outermost)
	router.Use(middleware.Recovery)
}
