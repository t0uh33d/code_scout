package app_test

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// Adding a dependency can raise the `go` directive in go.mod, because a module
// that requires a newer toolchain drags the whole build up with it. Nothing
// local notices: the CI workflow reads its Go version from go.mod, and a
// developer's toolchain is usually newer still. The Dockerfile is the one place
// that pins a version by hand, so it is the one place that silently falls
// behind, and it fails at image build time on a push rather than in any test.
//
// That has now happened twice. The first time the bump was caught by eye before
// pushing; the second, adding the MCP SDK moved the directive to 1.25.0 while
// the Dockerfile still said 1.24, every local test passed, and CI went red.
// Writing the rule down in the notes did not prevent the second one, so here it
// is as something that fails instead.
func TestTheDockerfileBuildsWithAToolchainGoModAccepts(t *testing.T) {
	required := goDirective(t)
	image := dockerfileGoVersion(t)

	if less(image, required) {
		t.Fatalf(
			"the Dockerfile builds on Go %d.%d but go.mod requires %d.%d or newer.\n"+
				"The image build fails with 'go.mod requires go >= %d.%d'.\n"+
				"Raise the FROM golang: line in the Dockerfile, and while you are there\n"+
				"the version is also stated in README.md, CONTRIBUTING.md and CLAUDE.md.",
			image[0], image[1], required[0], required[1], required[0], required[1])
	}
}

// goDirective returns the major and minor of go.mod's `go` line.
func goDirective(t *testing.T) [2]int {
	t.Helper()
	b, err := os.ReadFile("../go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	m := regexp.MustCompile(`(?m)^go (\d+)\.(\d+)`).FindStringSubmatch(string(b))
	if m == nil {
		t.Fatal("go.mod has no `go` directive, which should be impossible")
	}
	return pair(t, m)
}

// dockerfileGoVersion returns the major and minor of the build stage's base
// image, from a line like `FROM golang:1.25-alpine AS build`.
func dockerfileGoVersion(t *testing.T) [2]int {
	t.Helper()
	b, err := os.ReadFile("../Dockerfile")
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	m := regexp.MustCompile(`(?m)^FROM golang:(\d+)\.(\d+)`).FindStringSubmatch(string(b))
	if m == nil {
		t.Fatal("the Dockerfile has no `FROM golang:<major>.<minor>` line.\n" +
			"If the base image changed shape, this test needs to change with it,\n" +
			"because an unparsed version is the same as an unchecked one.")
	}
	return pair(t, m)
}

func pair(t *testing.T, m []string) [2]int {
	t.Helper()
	major, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("major version %q: %v", m[1], err)
	}
	minor, err := strconv.Atoi(m[2])
	if err != nil {
		t.Fatalf("minor version %q: %v", m[2], err)
	}
	return [2]int{major, minor}
}

// less reports whether a is an older Go release than b. A newer image than the
// directive asks for is fine, so this is not an equality check.
func less(a, b [2]int) bool {
	if a[0] != b[0] {
		return a[0] < b[0]
	}
	return a[1] < b[1]
}
