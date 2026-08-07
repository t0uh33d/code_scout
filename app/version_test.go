package app_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/getcodescout/code_scout/app"
)

// CI reads the section matching app.Version out of CHANGELOG.md and publishes
// it as the release notes, failing the release when there is no such section.
// That failure happens at tag time, which is the worst moment to find out. This
// test moves it to the commit that bumps the version.
func TestVersionHasAChangelogSection(t *testing.T) {
	changelog := readChangelog(t)

	want := "## [" + app.Version + "]"
	if !strings.Contains(changelog, want) {
		t.Fatalf("CHANGELOG.md has no %q section.\n"+
			"app.Version is %s, so a release cut today would publish empty notes.\n"+
			"Add the section, or move the entries under [Unreleased] into it.",
			want, app.Version)
	}
}

// The release job extracts notes by matching "## [version]" exactly. A heading
// written any other way — no brackets, or a "v" prefix — extracts nothing, and
// an empty extraction is indistinguishable from a release nobody documented.
func TestChangelogHeadingsAreTheShapeCIParses(t *testing.T) {
	heading := regexp.MustCompile(`(?m)^## .*$`)
	valid := regexp.MustCompile(`^## \[(Unreleased|\d+\.\d+\.\d+)\]( - \d{4}-\d{2}-\d{2})?$`)

	for _, h := range heading.FindAllString(readChangelog(t), -1) {
		if !valid.MatchString(h) {
			t.Errorf("changelog heading %q is not a shape CI can extract.\n"+
				"Use '## [1.2.3] - 2026-08-07', or '## [Unreleased]'.", h)
		}
	}
}

// Released sections carry a date; Unreleased must not. A dated Unreleased
// heading reads as shipped, and an undated release cannot be placed in time by
// anyone reading the notes later.
func TestOnlyUnreleasedHasNoDate(t *testing.T) {
	for _, line := range strings.Split(readChangelog(t), "\n") {
		switch {
		case line == "## [Unreleased]":
		case strings.HasPrefix(line, "## [Unreleased]"):
			t.Errorf("Unreleased carries a date: %q", line)
		case strings.HasPrefix(line, "## ["):
			if !strings.Contains(line, " - ") {
				t.Errorf("released section has no date: %q", line)
			}
		}
	}
}

func readChangelog(t *testing.T) string {
	t.Helper()
	// The test binary runs in the package directory, and app/ is one below the
	// repository root.
	b, err := os.ReadFile("../CHANGELOG.md")
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}
	return string(b)
}
