package view

import (
	"testing"

	"github.com/getcodescout/code_scout/internal/domain"
)

// A project with hundreds of tags must not push the log list off the screen,
// and must not hide the tag the list is currently filtered by.
func TestSplitTagsFoldsTheLongTail(t *testing.T) {
	var many []domain.TagCount
	for i := 0; i < 40; i++ {
		many = append(many, domain.TagCount{Tag: string(rune('a'+i%26)) + string(rune('0'+i/26)), Count: int64(40 - i)})
	}

	shown, rest := splitTags(many, domain.SearchFilter{})
	if len(shown) != visibleTags {
		t.Errorf("want %d chips shown, got %d", visibleTags, len(shown))
	}
	if len(rest) != len(many)-visibleTags {
		t.Errorf("want the remainder folded away, got %d of %d", len(rest), len(many))
	}
	// Ranked by count, so the ones shown are the ones the project uses.
	if shown[0].Tag != many[0].Tag {
		t.Errorf("the order changed: want %q first, got %q", many[0].Tag, shown[0].Tag)
	}

	// A handful stays a handful — no disclosure for six tags.
	few := many[:6]
	shown, rest = splitTags(few, domain.SearchFilter{})
	if len(shown) != 6 || len(rest) != 0 {
		t.Errorf("a short list should show whole, got %d shown and %d folded", len(shown), len(rest))
	}
}

// The load-bearing one. Clicking a rare tag filters the list by it; folding
// that chip away leaves the list narrowed by something you cannot see or undo.
func TestSplitTagsKeepsAnActiveTagVisible(t *testing.T) {
	var many []domain.TagCount
	for i := 0; i < 40; i++ {
		many = append(many, domain.TagCount{Tag: string(rune('a'+i%26)) + string(rune('0'+i/26)), Count: int64(40 - i)})
	}
	rare := many[len(many)-1].Tag

	for _, f := range []domain.SearchFilter{
		{Tags: []string{rare}},
		{ExcludeTags: []string{rare}},
	} {
		shown, rest := splitTags(many, f)

		var found bool
		for _, tc := range shown {
			if tc.Tag == rare {
				found = true
			}
		}
		if !found {
			t.Errorf("the tag the list is filtered by was folded away: %q", rare)
		}
		for _, tc := range rest {
			if tc.Tag == rare {
				t.Errorf("%q appears in both halves", rare)
			}
		}
	}
}

