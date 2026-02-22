package handlers

import (
	"context"
	"net/http"

	"github.com/t0uh33d/code_scout/view"
)

type ViewHandler struct{}

func NewViewHandler() *ViewHandler {
	return &ViewHandler{}
}

func (h *ViewHandler) BaseLayout(w http.ResponseWriter, r *http.Request) {
	c := view.BaseLayout("Code Scout")
	c.Render(context.Background(), w)
}

func (h *ViewHandler) Login(w http.ResponseWriter, r *http.Request) {
	c := view.Login()
	c.Render(context.Background(), w)
}
