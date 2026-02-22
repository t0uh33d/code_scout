package server

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/t0uh33d/code_scout/server/middleware"
)

func (s *Server) registerRoutes(router *mux.Router, opts ServerOpts) {
	// Serve static files (CSS, JS, images)
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("./view/static"))))

	// Public routes
	router.HandleFunc("/", opts.ViewHandler.BaseLayout).Methods("GET")
	router.HandleFunc("/login", opts.ViewHandler.Login).Methods("GET")

	// API subrouter with middleware chain
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
