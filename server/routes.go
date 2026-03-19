package server

import (
	"io/fs"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/t0uh33d/code_scout/server/middleware"
	"github.com/t0uh33d/code_scout/view/static"
)

func (s *Server) registerRoutes(router *mux.Router, opts ServerOpts) {
	// Serve embedded static files (CSS, JS, images)
	staticFS, _ := fs.Sub(static.Files, ".")
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Public routes — no session required
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

	// Log viewer routes — session protected
	webRouter.HandleFunc("/project/{id}/logs", opts.LogViewerHandler.LogViewer).Methods("GET")
	webRouter.HandleFunc("/project/{id}/logs/partial", opts.LogViewerHandler.LogsPartial).Methods("GET")
	webRouter.HandleFunc("/project/{id}/session/{sid}", opts.LogViewerHandler.SessionTimeline).Methods("GET")
	webRouter.HandleFunc("/project/{id}/network/{rid}", opts.LogViewerHandler.NetworkDetail).Methods("GET")
	webRouter.HandleFunc("/dashboard/projects/{id}/stats", opts.LogViewerHandler.ProjectStats).Methods("GET")

	// SSE streaming — session protected
	webRouter.HandleFunc("/stream/logs", opts.LogViewerHandler.StreamLogs).Methods("GET")

	// Export — session protected
	webRouter.HandleFunc("/export/logs", opts.ExportHandler.ExportLogs).Methods("GET")

	// API subrouter with middleware chain (project/log SDK auth)
	apiRouter := router.PathPrefix("/api").Subrouter()
	apiRouter.Use(middleware.HttpLogger)
	apiRouter.Use(middleware.ConnectionCloseMiddleware)
	apiRouter.Use(middleware.CorsMiddleware)
	apiRouter.Use(middleware.JsonContentTypeMiddleware)
	apiRouter.Use(middleware.Authenticate(s.projectSvc))

	// Protected API routes
	apiRouter.HandleFunc("/validate", opts.ProjectHandler.Validate).Methods("GET")
	apiRouter.HandleFunc("/project", opts.ProjectHandler.CreateProject).Methods("POST")
	apiRouter.HandleFunc("/project/{project_id}", opts.ProjectHandler.DeleteProject).Methods("DELETE")
	apiRouter.HandleFunc("/logs/dump", opts.LogHandler.DumpLogs).Methods("POST")

	// Panic recovery (outermost)
	router.Use(middleware.Recovery)
}
