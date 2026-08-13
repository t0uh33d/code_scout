package db

import "testing"

// The search box is the only place a user types these characters, and every one
// of them used to reach LIKE as a wildcard.
func TestContainsPatternEscapesWildcards(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain text is only wrapped", "timeout", `%timeout%`},
		{"percent is literal", "100%", `%100\%%`},
		{"underscore is literal", "user_id", `%user\_id%`},
		{"backslash is literal", `C:\tmp`, `%C:\\tmp%`},
		// The backslash has to be escaped before the others
		{"backslash before a percent", `50\%`, `%50\\\%%`},
		{"empty stays a match-all", "", `%%`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := containsPattern(c.in); got != c.want {
				t.Errorf("containsPattern(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
