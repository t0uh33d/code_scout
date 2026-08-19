package middleware

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func headersFor(t *testing.T, path string, tweak func(*http.Request)) http.Header {
	t.Helper()

	var got http.Header
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = w.Header()
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, path, nil)
	if tweak != nil {
		tweak(r)
	}
	h.ServeHTTP(httptest.NewRecorder(), r)
	return got
}

// The dashboard holds a session cookie, forms that rotate secrets and delete
// projects, and a live view of somebody's phone. A browser assumes the
// permissive answer to every one of these unless told otherwise, and it was
// never told.
func TestTheDashboardCarriesItsSecurityHeaders(t *testing.T) {
	h := headersFor(t, "/project/abc/logs", nil)

	for header, want := range map[string]string{
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	} {
		if got := h.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}

	if h.Get("Permissions-Policy") == "" {
		t.Error("Permissions-Policy is unset")
	}
	if h.Get("Content-Security-Policy") == "" {
		t.Error("no CSP on an HTML route")
	}
}

// Clickjacking is the concrete one: the live screen and the danger-zone forms
// are exactly what an attacker would put a transparent button over.
func TestTheDashboardRefusesToBeFramed(t *testing.T) {
	h := headersFor(t, "/settings", nil)

	if h.Get("X-Frame-Options") != "DENY" {
		t.Error("X-Frame-Options is not DENY")
	}
	if !strings.Contains(h.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Error("the CSP does not forbid framing, and it is the directive " +
			"modern browsers actually read")
	}
}

// A CSP describes what an HTML document may load. On a JSON body it is noise,
// and noise in a header is what gets a whole middleware deleted later.
func TestApiRoutesGetNoCSP(t *testing.T) {
	for _, path := range []string{"/api/logs/dump", "/api/mcp", "/api/validate"} {
		if csp := headersFor(t, path, nil).Get("Content-Security-Policy"); csp != "" {
			t.Errorf("%s carries a CSP: %q", path, csp)
		}
	}

	// The rest still applies: nosniff matters most where a body is echoed.
	if headersFor(t, "/api/logs/dump", nil).Get("X-Content-Type-Options") != "nosniff" {
		t.Error("the API lost nosniff")
	}
}

// HSTS pins a browser to HTTPS. Sent by a plain-HTTP dev instance it locks the
// operator out of their own dashboard until they clear it by hand.
func TestHSTSOnlyOverTLS(t *testing.T) {
	if got := headersFor(t, "/", nil).Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS on plain HTTP: %q", got)
	}

	direct := headersFor(t, "/", func(r *http.Request) { r.TLS = &tls.ConnectionState{} })
	if direct.Get("Strict-Transport-Security") == "" {
		t.Error("no HSTS on a TLS request")
	}

	proxied := headersFor(t, "/", func(r *http.Request) {
		r.Header.Set("X-Forwarded-Proto", "https")
	})
	if proxied.Get("Strict-Transport-Security") == "" {
		t.Error("no HSTS behind a TLS-terminating proxy, which is how this is " +
			"almost always deployed")
	}
}

// The CSP has to allow what the dashboard actually loads. One that breaks the
// product gets switched off rather than corrected, which is worse than none.
func TestTheCSPAllowsTheMechanismsTheDashboardUses(t *testing.T) {
	csp := headersFor(t, "/", nil).Get("Content-Security-Policy")

	for _, need := range []string{
		// hx-on and the inline scripts on the live and log viewer screens.
		"'unsafe-inline'",
		// SSE for the log tail and the live watcher, WebSocket for the device.
		"ws:",
		"wss:",
		// Tailwind emits inline styles, and the wordmark is a data: image.
		"data:",
	} {
		if !strings.Contains(csp, need) {
			t.Errorf("the CSP omits %q, which the dashboard needs:\n%s", need, csp)
		}
	}
}

// Derived from the templates rather than from a list kept by hand.
//
// Written this way because the hand-kept version was wrong on the first
// attempt: it named cdn.jsdelivr.net and forgot both Google Fonts origins, so
// the policy would have blocked every stylesheet and font file the dashboard
// loads. A list of origins in a test is exactly as likely to drift as the one
// in the header it is checking.
func TestTheCSPCoversEveryOriginTheViewsUse(t *testing.T) {
	views, err := filepath.Glob(filepath.Join("..", "..", "view", "*.templ"))
	if err != nil || len(views) == 0 {
		t.Fatalf("no templates found to check against: %v", err)
	}

	// src="https://…" and href="https://…", which is every way a template
	// pulls something in from off-origin.
	ref := regexp.MustCompile(`(?:src|href)="(https?://[^"/]+)`)

	origins := map[string]string{}
	for _, path := range views {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, m := range ref.FindAllStringSubmatch(string(body), -1) {
			origins[m[1]] = filepath.Base(path)
		}
	}
	if len(origins) == 0 {
		t.Skip("the templates load nothing off-origin, so there is nothing to cover")
	}

	csp := headersFor(t, "/", nil).Get("Content-Security-Policy")
	for origin, where := range origins {
		if !strings.Contains(csp, origin) {
			t.Errorf("%s loads %s and the CSP does not allow it, so that "+
				"request will be blocked in every browser:\n%s", where, origin, csp)
		}
	}
}
