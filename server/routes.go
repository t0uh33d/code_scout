package server

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/t0uh33d/code_scout/server/middleware"
)

func (s *Server) registerRoutes(router *mux.Router, opts ServerOpts) {
	// Serve static files (CSS, JS, images)
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("./view/static"))))

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
	webRouter.HandleFunc("/", opts.ViewHandler.BaseLayout).Methods("GET")

	// API subrouter with middleware chain (project/log SDK auth)
	apiRouter := router.PathPrefix("/api").Subrouter()
	apiRouter.Use(middleware.HttpLogger)
	apiRouter.Use(middleware.ConnectionCloseMiddleware)
	apiRouter.Use(middleware.CorsMiddleware)
	apiRouter.Use(middleware.JsonContentTypeMiddleware)
	apiRouter.Use(middleware.Authenticate(s.projectRepo, s.db))

	// Protected API routes
	apiRouter.HandleFunc("/validate", opts.ProjectHandler.Validate).Methods("GET")
	apiRouter.HandleFunc("/project", opts.ProjectHandler.CreateProject).Methods("POST")
	apiRouter.HandleFunc("/project/{project_id}", opts.ProjectHandler.DeleteProject).Methods("DELETE")
	apiRouter.HandleFunc("/logs/dump", opts.LogHandler.DumpLogs).Methods("POST")

	// Panic recovery (outermost)
	router.Use(middleware.Recovery)
}
