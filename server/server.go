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
	"github.com/t0uh33d/code_scout/internal/ports"
	"github.com/t0uh33d/code_scout/pkg/cslog"
	"github.com/t0uh33d/code_scout/server/handlers"
)

type ServerOpts struct {
	Host             string
	Port             int
	ProjectSvc       ports.ProjectManager
	AuthSvc          ports.AuthManager
	ProjectHandler   *handlers.ProjectHandler
	LogHandler       *handlers.LogHandler
	ViewHandler      *handlers.ViewHandler
	AuthHandler      *handlers.AuthHandler
	DashboardHandler *handlers.DashboardHandler
}

type Server struct {
	host       string
	port       int
	projectSvc ports.ProjectManager
	srvr       *http.Server
}

func New(opts ServerOpts) *Server {
	s := &Server{
		host:       opts.Host,
		port:       opts.Port,
		projectSvc: opts.ProjectSvc,
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

func (s *Server) Run() {
	cslog.Info(fmt.Sprintf("Start Code Scout API at :%d", s.port))
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
