package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/getcodescout/code_scout/internal/ports"
	"github.com/getcodescout/code_scout/internal/services"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"github.com/getcodescout/code_scout/server/handlers"
	"github.com/getcodescout/code_scout/server/middleware"
	"gorm.io/gorm"
)

type ServerOpts struct {
	Host                    string
	Port                    int
	ProjectSvc              ports.ProjectManager
	AuthSvc                 ports.AuthManager
	MemberSvc               *services.MemberService
	ProjectHandler          *handlers.ProjectHandler
	LogHandler              *handlers.LogHandler
	ViewHandler             *handlers.ViewHandler
	AuthHandler             *handlers.AuthHandler
	DashboardHandler        *handlers.DashboardHandler
	LogViewerHandler        *handlers.LogViewerHandler
	ProjectSettingsHandler  *handlers.ProjectSettingsHandler
	MemberHandler           *handlers.MemberHandler
	InstanceSettingsHandler *handlers.InstanceSettingsHandler
	AccountHandler          *handlers.AccountHandler
	ExportHandler           *handlers.ExportHandler
	LiveHandler             *handlers.LiveHandler
	// TokenSvc authenticates the MCP endpoint; MCPHandler serves it.
	TokenSvc   middleware.TokenAuthenticator
	MCPHandler http.Handler
	DB         *gorm.DB
}

type Server struct {
	host       string
	port       int
	projectSvc ports.ProjectManager
	db         *gorm.DB
	srvr       *http.Server
}

func New(opts ServerOpts) *Server {
	s := &Server{
		host:       opts.Host,
		port:       opts.Port,
		projectSvc: opts.ProjectSvc,
		db:         opts.DB,
	}

	router := mux.NewRouter()
	s.registerRoutes(router, opts)

	s.srvr = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      router,
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return s
}

// Handler exposes the fully-wired mux, so integration tests can serve the
// real thing with httptest instead of binding a port.
func (s *Server) Handler() http.Handler {
	return s.srvr.Handler
}

func (s *Server) Run() {
	cslog.Info(fmt.Sprintf("Start CodeScout API at :%d", s.port))
	if err := s.srvr.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		cslog.Error("Server error: ", err.Error())
		syscall.Kill(syscall.Getpid(), syscall.SIGINT)
	}
}

func (s *Server) HandleGracefulShutdown() error {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	cslog.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := s.srvr.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown failed: %w", err)
	}

	cslog.Info("Server exited")
	return nil
}
