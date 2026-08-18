package handlers

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The session cookie carries a bearer credential for thirty days, so its flags
// are part of the security posture rather than a detail. Pinned here because
// they are five words in a struct literal that nothing else would notice going
// missing.
func TestTheSessionCookieCarriesItsFlags(t *testing.T) {
	plain := httptest.NewRequest(http.MethodPost, "/api/auth/submit", nil)
	c := sessionCookie(plain, "tok", time.Now().Add(time.Hour), 0)

	if !c.HttpOnly {
		t.Error("HttpOnly is off: script on the page could read the session token")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite is %v, want Lax", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("Path is %q, want /", c.Path)
	}
}

// Secure follows the scheme. Hardcoded on, the cookie is dropped by a
// plain-HTTP dev instance and by the LAN address a phone is pointed at during
// setup; hardcoded off, one downgraded request puts a live token in cleartext.
func TestSecureFollowsTheScheme(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  func() *http.Request
		want bool
	}{
		{
			name: "plain http",
			req:  func() *http.Request { return httptest.NewRequest(http.MethodGet, "/", nil) },
			want: false,
		},
		{
			name: "direct TLS",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.TLS = &tls.ConnectionState{}
				return r
			},
			want: true,
		},
		{
			name: "TLS terminated by a proxy",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.Header.Set("X-Forwarded-Proto", "https")
				return r
			},
			want: true,
		},
		{
			name: "proxy header in the other case",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.Header.Set("X-Forwarded-Proto", "HTTPS")
				return r
			},
			want: true,
		},
		{
			name: "proxy reports plain http",
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/", nil)
				r.Header.Set("X-Forwarded-Proto", "http")
				return r
			},
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sessionCookie(tc.req(), "tok", time.Now(), 0).Secure; got != tc.want {
				t.Errorf("Secure = %v, want %v", got, tc.want)
			}
		})
	}
}

// A browser keys a cookie on name plus flags, so a clear whose flags differ
// from the write leaves the original in place and the token keeps being sent.
func TestTheClearMatchesTheWrite(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	r.Header.Set("X-Forwarded-Proto", "https")

	write := sessionCookie(r, "tok", time.Now().Add(time.Hour), 0)
	clear := sessionCookie(r, "", time.Unix(0, 0), -1)

	if clear.Secure != write.Secure || clear.HttpOnly != write.HttpOnly ||
		clear.Path != write.Path || clear.SameSite != write.SameSite {
		t.Errorf("clear and write disagree on flags:\n write %+v\n clear %+v", write, clear)
	}
	if clear.MaxAge >= 0 {
		t.Errorf("MaxAge is %d, want negative so the browser drops it", clear.MaxAge)
	}
	if clear.Value != "" {
		t.Errorf("the clear still carries a value: %q", clear.Value)
	}
}
