package view

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/getcodescout/code_scout/internal/domain"
)

// The snippet the dashboard hands somebody the moment they create a project is
// the first CodeScout code most people will ever run, and for a long time it
// omitted `sync:`.
//
// That field has no default. Without it the SDK's upload worker never starts,
// and because the SQLite write is the uploader's queue rather than a store
// anyone reads back, the logs are not queued for later either: they are
// dropped. So a developer followed the dashboard's own instructions exactly,
// saw a working console and a working in-app panel, and waited for rows that
// were never going to arrive, with nothing anywhere saying why.
func TestTheSetupSnippetConfiguresAnUploader(t *testing.T) {
	id := uuid.New()
	snippet := setupSnippet(&domain.ProjectDetails{ID: id, SecretKey: "sekrit"}, "https://logs.example.com")

	if !strings.Contains(snippet, "sync:") {
		t.Fatalf("the setup snippet has no sync: block, so an app that copies it uploads nothing:\n%s", snippet)
	}
	if !strings.Contains(snippet, "LogSyncBehavior(") {
		t.Fatalf("sync: must be given a LogSyncBehavior, got:\n%s", snippet)
	}
}

// The rest of the snippet is what makes it work at all, so it is worth pinning
// alongside: a credential block with the caller's own project in it.
func TestTheSetupSnippetCarriesTheProjectsOwnCredentials(t *testing.T) {
	id := uuid.New()
	snippet := setupSnippet(&domain.ProjectDetails{ID: id, SecretKey: "sekrit"}, "https://logs.example.com")

	for _, want := range []string{
		"CodeScout.instance.init(",
		"freshContextFetcher:",
		"projectCredentials: ProjectCredentials(",
		"https://logs.example.com/",
		id.String(),
		"sekrit",
	} {
		if !strings.Contains(snippet, want) {
			t.Errorf("setup snippet is missing %q:\n%s", want, snippet)
		}
	}
}
