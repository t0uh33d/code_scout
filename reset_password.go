package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	confs "github.com/getcodescout/code_scout/conf"
	dbadapter "github.com/getcodescout/code_scout/internal/adapters/db"
	"github.com/getcodescout/code_scout/internal/services"
	"github.com/getcodescout/code_scout/pkg/cslog"
)

// runResetPassword implements `code_scout reset-password --email=...`.
//
// It prints the new temporary password to stdout exactly once. Anyone who can
// run this can take over any account, which is the point: the trust boundary
// is shell access to the machine the server runs on.
func runResetPassword(args []string) int {
	fs := flag.NewFlagSet("reset-password", flag.ExitOnError)
	email := fs.String("email", "", "email of the account to reset (required)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: code_scout reset-password --email=<email>")
		fmt.Fprintln(os.Stderr, "\nSets a new temporary password for the account and signs it out everywhere.")
		fmt.Fprintln(os.Stderr, "Reads the same configuration as the server (env vars / config file).")
		fs.PrintDefaults()
	}
	fs.Parse(args)

	if *email == "" {
		fs.Usage()
		return 2
	}

	// The temporary password is the entire output of this command; server-style
	// debug logging would bury it.
	cslog.GetLogger().SetLevel(logrus.ErrorLevel)

	if err := confs.Load(); err != nil {
		fmt.Fprintln(os.Stderr, "configuration error:", err)
		return 1
	}

	db, err := dbadapter.NewConnection(dbadapter.DBConfig{
		User:            confs.Conf.DBUser,
		Password:        confs.Conf.DBPassword,
		Database:        confs.Conf.DBName,
		Host:            confs.Conf.DBHost,
		Port:            confs.Conf.DBPort,
		SSLMode:         confs.Conf.DBSSLMode,
		MaxOpenConns:    2,
		MaxIdleConns:    1,
		ConnMaxLifetime: time.Minute,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "database error:", err)
		return 1
	}

	authSvc := services.NewAuthService(dbadapter.NewUserRepo(db))
	tempPassword, err := authSvc.ResetPassword(context.Background(), *email)
	if err != nil {
		fmt.Fprintln(os.Stderr, "reset failed:", err)
		return 1
	}

	fmt.Printf("Temporary password for %s (shown once, not stored):\n\n    %s\n\n", *email, tempPassword)
	// No "now change it in the dashboard" instruction yet: the change-password
	// screen ships with Members in slice 4.
	fmt.Println("All sessions for this account have been signed out.")
	return 0
}
