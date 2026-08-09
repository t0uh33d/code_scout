package view

import (
	"strings"
	"testing"

	"github.com/getcodescout/code_scout/internal/domain"
)

// An empty favourites tab is not an empty instance.
//
// Landing on ?filter=favorites with nothing starred used to draw the "no
// projects yet" screen: it says something false, and it takes the tabs away
// with it, so there is no way back to the list from the page you are on.
func TestAnEmptyFavouritesTabStillShowsTheTabs(t *testing.T) {
	html := render(t, Dashboard(DashboardData{
		User:     &domain.User{Name: "T", Email: "t@test.local"},
		Projects: &domain.ProjectListResult{},
		Filter:   "favorites",
	}))

	if !strings.Contains(html, "No favourites yet") {
		t.Error("an empty favourites tab did not say which list was empty")
	}
	if strings.Contains(html, "No projects yet") {
		t.Error("an empty favourites tab claimed the instance has no projects")
	}
	// The way back. Without the toolbar there is no control left to undo the
	// filter that emptied the list.
	if !strings.Contains(html, "All projects") {
		t.Error("the tabs are gone, so the filter cannot be cleared")
	}
}

// The genuinely empty instance still gets its own screen, or the assertion
// above would pass with the filter check removed.
func TestAnInstanceWithNoProjectsSaysSo(t *testing.T) {
	html := render(t, Dashboard(DashboardData{
		User:     &domain.User{Name: "T", Email: "t@test.local"},
		Projects: &domain.ProjectListResult{},
		Filter:   "all",
	}))

	if !strings.Contains(html, "No projects yet") {
		t.Error("a new instance did not get the empty state")
	}
}

func TestIsLoopbackURL(t *testing.T) {
	cases := map[string]bool{
		"http://localhost:24275": true,
		"http://127.0.0.1:24275": true,
		"http://0.0.0.0:24275":   true,
		"https://localhost":      true,
		"http://[::1]:24275":     true,
		// Host headers arrive however the user typed them.
		"http://LocalHost:24275":      true,
		"http://192.168.1.42:24275":   false,
		"https://scout.example.com":   false,
		"https://logs.internal/scout": false,
		// A host that merely contains "localhost" is a different machine.
		"https://localhost.example.com": false,
	}
	for url, want := range cases {
		if got := isLoopbackURL(url); got != want {
			t.Errorf("isLoopbackURL(%q) = %v, want %v", url, got, want)
		}
	}
}
