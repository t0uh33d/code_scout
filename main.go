package main

import (
	"net/http"
	"os"
	"strconv"

	"github.com/gorilla/mux"

	confs "github.com/t0uh33d/code_scout/conf"
	"github.com/t0uh33d/code_scout/ctrls"
	"github.com/t0uh33d/code_scout/jobs"
	"github.com/t0uh33d/code_scout/middleware"
	"github.com/t0uh33d/code_scout/utils"
	"github.com/t0uh33d/code_scout/utils/cslog"
)

var BuildTime = "-"
var BranchName = "-"
var CommitHash = "-"
var DirtyFiles = "-"

func main() {
	reqID := cslog.RequestID("code-scout-service-startup")
	req := cslog.NewRequestLog(cslog.RequestLog{
		RequestID: reqID,
	})
	log := cslog.NewRequestLog(req)
	log.Info("Starting User panel...")
	log.Info("Built  @", BuildTime)
	log.Info("Branch $", BranchName)
	log.Info("Commit #", CommitHash)
	log.Info("DirtyFiles *", DirtyFiles)

	router := mux.NewRouter()

	router.HandleFunc("/", ctrls.BaseLayout)

	apiRouter := router.PathPrefix("/api").Subrouter()
	apiRouter.Use(cslog.HttpLogger)
	apiRouter.Use(utils.CloseConnectionMiddleware)
	apiRouter.Use(utils.CorsMiddleware)
	apiRouter.Use(utils.JsonContentTypeMiddleware)
	apiRouter.Use(middleware.Authenticate)

	apiRouter.HandleFunc("/validate", ctrls.Validate).Methods("GET")
	apiRouter.HandleFunc("/project", ctrls.CreateProject).Methods("POST")
	apiRouter.HandleFunc("/project/{project_id}", ctrls.DeleteProject).Methods("DELETE")
	apiRouter.HandleFunc("/logs/dump", ctrls.DumpLogs).Methods("POST")

	// Crons or jobs
	sc := jobs.NewSchedulerCtrls(cslog.RequestLog{
		RequestID: reqID,
	})

	go sc.Scheduler()

	port := os.Getenv("PORT")
	if port == "" {
		port = strconv.Itoa(confs.Conf.ServerPort)
	}

	host := os.Getenv("HOST")
	if host == "" {
		host = confs.Conf.ServerHost
	}

	log.Info("Start Code Scout API at  :" + port)

	// http.ListenAndServe(":"+port, router)
	if err := http.ListenAndServe(":"+port, router); err != nil {
		log.Error("Server failed to start: ", err.Error())
		os.Exit(1)
	}
}
