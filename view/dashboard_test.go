package view

import "testing"

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
