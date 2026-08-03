package server

import (
	"io/fs"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/getcodescout/code_scout/server/handlers"
	"github.com/getcodescout/code_scout/server/middleware"
	"github.com/getcodescout/code_scout/view/static"
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
	// Session protected, but deliberately the one page RequireSession still lets
	// through when the account is on a temporary password.
	webRouter.HandleFunc("/change-password", opts.AuthHandler.ChangePasswordPage).Methods("GET")
	webRouter.HandleFunc("/change-password", opts.AuthHandler.ChangePassword).Methods("POST")

	// Dashboard HTMX partial routes — session protected
	webRouter.HandleFunc("/dashboard/projects", opts.DashboardHandler.ProjectsGrid).Methods("GET")
	webRouter.HandleFunc("/dashboard/projects", opts.DashboardHandler.CreateProject).Methods("POST")
	// POST as well as GET: the Access step's Back button sends the draft along
	// so returning to step 1 does not empty the fields.
	webRouter.HandleFunc("/dashboard/projects/new", opts.DashboardHandler.NewProjectWizard).Methods("GET", "POST")
	webRouter.HandleFunc("/dashboard/projects/access", opts.DashboardHandler.ProjectWizardAccess).Methods("POST")
	webRouter.HandleFunc("/dashboard/projects/list", opts.DashboardHandler.ProjectsListPartial).Methods("GET")
	// Everything scoped to one project hangs off this subrouter, so access is
	// checked once in middleware rather than in each handler. A handler that
	// forgot the check would leak another team's logs, and nothing catches a
	// check that was never written.
	//
	// A project the caller cannot see answers 404, never 403: they should not
	// learn it exists.
	projectRouter := webRouter.PathPrefix("/project/{id}").Subrouter()
	projectRouter.Use(middleware.RequireProjectAccess(opts.MemberSvc))

	projectRouter.HandleFunc("/overview", opts.LogViewerHandler.Overview).Methods("GET")
	projectRouter.HandleFunc("/logs", opts.LogViewerHandler.LogViewer).Methods("GET")
	projectRouter.HandleFunc("/errors", opts.LogViewerHandler.Errors).Methods("GET")
	projectRouter.HandleFunc("/network", opts.LogViewerHandler.Network).Methods("GET")
	projectRouter.HandleFunc("/network/inspector", opts.LogViewerHandler.NetworkInspector).Methods("GET")
	projectRouter.HandleFunc("/sessions", opts.LogViewerHandler.Sessions).Methods("GET")
	projectRouter.HandleFunc("/devices", opts.LogViewerHandler.Devices).Methods("GET")
	projectRouter.HandleFunc("/device/{iid}", opts.LogViewerHandler.DeviceDetail).Methods("GET")
	projectRouter.HandleFunc("/logs/partial", opts.LogViewerHandler.LogsPartial).Methods("GET")
	projectRouter.HandleFunc("/session/{sid}", opts.LogViewerHandler.SessionTimeline).Methods("GET")
	projectRouter.HandleFunc("/network/{rid}", opts.LogViewerHandler.NetworkDetail).Methods("GET")
	// Stats and favourites moved here from /dashboard/projects/{id}/... so they
	// inherit the access check. They are project data like anything else.
	projectRouter.HandleFunc("/stats", opts.LogViewerHandler.ProjectStats).Methods("GET")
	projectRouter.HandleFunc("/favorite", opts.DashboardHandler.ToggleFavorite).Methods("POST")

	// Live devices and streaming. Read access is enough for all of it: a live
	// stream shows the same logs the log viewer already does, so anyone who can
	// read the project can watch, pair a device, or end a session.
	projectRouter.HandleFunc("/live", opts.LiveHandler.LiveDevices).Methods("GET")
	projectRouter.HandleFunc("/live/list", opts.LiveHandler.LiveDevicesList).Methods("GET")
	projectRouter.HandleFunc("/live/new", opts.LiveHandler.NewLiveSession).Methods("POST")
	// Registered before /live/{sid} so the fixed paths above always win. Mux
	// matches in order, and "list" would otherwise be read as a session id.
	projectRouter.HandleFunc("/live/{sid}", opts.LiveHandler.LiveStream).Methods("GET")
	projectRouter.HandleFunc("/live/{sid}/events", opts.LiveHandler.WatchStream).Methods("GET")
	projectRouter.HandleFunc("/live/{sid}/end", opts.LiveHandler.EndLiveSession).Methods("POST")

	// Settings is readable by anyone on the project; changing anything needs
	// maintainer, and deleting additionally needs the Admin role.
	projectRouter.HandleFunc("/settings", opts.ProjectSettingsHandler.Settings).Methods("GET")

	manageRouter := projectRouter.NewRoute().Subrouter()
	manageRouter.Use(middleware.RequireProjectManage)
	manageRouter.HandleFunc("/settings/general", opts.ProjectSettingsHandler.UpdateGeneral).Methods("POST")
	manageRouter.HandleFunc("/settings/secret", opts.ProjectSettingsHandler.RevealSecret).Methods("GET")
	manageRouter.HandleFunc("/settings/secret/rotate", opts.ProjectSettingsHandler.RotateSecret).Methods("POST")
	manageRouter.HandleFunc("/settings/confirm", opts.ProjectSettingsHandler.ConfirmDialog).Methods("GET")
	manageRouter.HandleFunc("/settings/access", opts.ProjectSettingsHandler.SetAccess).Methods("POST")
	manageRouter.HandleFunc("/settings/access/remove", opts.ProjectSettingsHandler.RemoveAccess).Methods("POST")

	deleteRouter := projectRouter.NewRoute().Subrouter()
	deleteRouter.Use(middleware.RequireProjectDelete)
	deleteRouter.HandleFunc("/settings/delete", opts.ProjectSettingsHandler.DeleteProject).Methods("POST")

	// Instance settings change how every project renders, so they sit behind
	// the super admin role rather than with anyone who can create projects.
	// The page itself is readable by anyone signed in — its tabs decide what
	// each role actually sees. Changing an instance-wide setting is the part
	// that needs the super admin.
	webRouter.HandleFunc("/settings", opts.InstanceSettingsHandler.Settings).Methods("GET")

	instanceRouter := webRouter.NewRoute().Subrouter()
	instanceRouter.Use(middleware.RequireSuperAdmin)
	instanceRouter.HandleFunc("/settings/display", opts.InstanceSettingsHandler.UpdateDisplay).Methods("POST")
	instanceRouter.HandleFunc("/settings/retention", opts.InstanceSettingsHandler.UpdateRetention).Methods("POST")
	instanceRouter.HandleFunc("/settings/limits", opts.InstanceSettingsHandler.UpdateLimits).Methods("POST")

	// Members is instance scoped, so it lives outside /project.
	webRouter.HandleFunc("/members", opts.MemberHandler.Members).Methods("GET")
	webRouter.HandleFunc("/members", opts.MemberHandler.CreateMember).Methods("POST")
	webRouter.HandleFunc("/members/new", opts.MemberHandler.NewMember).Methods("GET")
	webRouter.HandleFunc("/members/{id}/reset", opts.MemberHandler.ResetPassword).Methods("POST")
	webRouter.HandleFunc("/members/{id}/role", opts.MemberHandler.ChangeRole).Methods("POST")
	webRouter.HandleFunc("/members/{id}/delete", opts.MemberHandler.DeleteMember).Methods("POST")

	// Streaming and export take the project id as a QUERY parameter, so they do
	// not match the /project/{id} subrouter and need the check applied
	// explicitly. Both hand back a project's contents, so missing this would let
	// anyone signed in stream or export any project by guessing its id.
	projectQueryRouter := webRouter.NewRoute().Subrouter()
	projectQueryRouter.Use(middleware.RequireProjectAccessFromQuery(opts.MemberSvc, "project_id"))
	projectQueryRouter.HandleFunc("/stream/logs", opts.LogViewerHandler.StreamLogs).Methods("GET")
	projectQueryRouter.HandleFunc("/export/logs", opts.ExportHandler.ExportLogs).Methods("GET")

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
	// The device's end of a live session. Behind the same credential check as
	// everything else here, so the pairing code decides which session a device
	// joins, never whether it is allowed to join one at all.
	apiRouter.HandleFunc("/live/socket", opts.LiveHandler.DeviceSocket).Methods("GET")

	// Panic recovery (outermost)
	router.Use(middleware.Recovery)
}
