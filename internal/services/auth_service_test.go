package services

import "testing"

func TestNormalizeEmail(t *testing.T) {
	cases := map[string]struct {
		want string
		ok   bool
	}{
		"bob@x.com":     {"bob@x.com", true},
		"  BOB@X.com  ": {"bob@x.com", true},
		// RFC 5322 name-addr forms must collapse to the bare address, or the
		// stored identifier can never log in from the email input.
		"Bob Smith <bob@x.com>":  {"bob@x.com", true},
		"<admin@example.com>":    {"admin@example.com", true},
		"admin@example.com (hi)": {"admin@example.com", true},
		"not-an-email":           {"", false},
		"":                       {"", false},
		"a@b@c":                  {"", false},
	}
	for in, tc := range cases {
		got, err := normalizeEmail(in)
		if tc.ok != (err == nil) {
			t.Errorf("normalizeEmail(%q) error = %v, want ok=%v", in, err, tc.ok)
			continue
		}
		if got != tc.want {
			t.Errorf("normalizeEmail(%q) = %q, want %q", in, got, tc.want)
		}
	}
}
