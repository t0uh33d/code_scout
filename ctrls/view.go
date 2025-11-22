package ctrls

import (
	"context"
	"net/http"

	"github.com/t0uh33d/code_scout/view"
)

func BaseLayout(w http.ResponseWriter, r *http.Request) {
	c := view.BaseLayout("Code Scout")

	c.Render(context.Background(), w)
}
