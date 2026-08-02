package view

import (
	"regexp"
	"strings"
	"testing"
)

var classAttr = regexp.MustCompile(`class="([^"]*)"`)

// The sidebar heading is rendered twice: in place by the shell, and as the
// out-of-band fragment a rename swaps in. They used to be two hand-written
// copies, and they drifted — the fragment kept text-base after the sidebar moved
// to text-lg, so renaming a project silently shrank the heading. Both now go
// through projectShellName, and this pins that they cannot diverge again.
func TestShellNameFragmentMatchesTheSidebarHeading(t *testing.T) {
	inPlace := render(t, projectShellName("Checkout", false))
	swapped := render(t, ShellNameOOB("Checkout"))

	inPlaceClass := classAttr.FindStringSubmatch(inPlace)
	swappedClass := classAttr.FindStringSubmatch(swapped)
	if inPlaceClass == nil || swappedClass == nil {
		t.Fatalf("both renders need a class attribute\n in place: %s\n swapped: %s", inPlace, swapped)
	}
	if inPlaceClass[1] != swappedClass[1] {
		t.Errorf("the swapped-in heading would not match the sidebar's\n in place: %q\n swapped:  %q",
			inPlaceClass[1], swappedClass[1])
	}
}

// hx-swap-oob is what makes the swap land, and it must be on the fragment only.
// On the in-place heading it would make the page try to swap itself on load.
func TestShellNameOOBMarking(t *testing.T) {
	if got := render(t, ShellNameOOB("Checkout")); !contains(got, `hx-swap-oob="true"`) {
		t.Errorf("the rename fragment must carry hx-swap-oob, got %s", got)
	}
	if got := render(t, projectShellName("Checkout", false)); contains(got, "hx-swap-oob") {
		t.Errorf("the in-place heading must not carry hx-swap-oob, got %s", got)
	}
}

// The id is the only thing connecting the fragment to the element it replaces.
// hx-swap-oob fails silently when it does not match, so it is worth pinning.
func TestShellNameKeepsItsID(t *testing.T) {
	for name, out := range map[string]string{
		"in place": render(t, projectShellName("Checkout", false)),
		"swapped":  render(t, ShellNameOOB("Checkout")),
	} {
		if !contains(out, `id="project-shell-name"`) {
			t.Errorf("%s heading lost its id, got %s", name, out)
		}
	}
}

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }
