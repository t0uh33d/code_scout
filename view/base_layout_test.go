package view

import (
	"strings"
	"testing"

	"github.com/getcodescout/code_scout/view/static"
)

// The link and the embedded file have to agree. A favicon referenced but not
// embedded is a 404 on every page, and nothing else in the suite would notice.
func TestBaseLayoutFavicon(t *testing.T) {
	const path = "images/favicon.png"

	html := render(t, BaseLayout("Overview"))
	if !strings.Contains(html, `href="/static/`+path+`"`) {
		t.Errorf("base layout does not link %s", path)
	}

	if _, err := static.Files.ReadFile(path); err != nil {
		t.Errorf("%s is linked but not embedded: %v", path, err)
	}
}
