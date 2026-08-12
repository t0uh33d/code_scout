package handlers

import (
	"net/http"
	"time"

	"github.com/getcodescout/code_scout/internal/services"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"github.com/getcodescout/code_scout/pkg/utils"
	"github.com/getcodescout/code_scout/server/middleware"
	"github.com/getcodescout/code_scout/view"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// APITokenHandler drives the API tokens pane on /settings. The owner of every
// operation is the signed-in user from the context, never a field in the
// request: a form cannot mint or revoke for somebody else.
type APITokenHandler struct {
	tokenSvc *services.TokenService
}

func NewAPITokenHandler(tokenSvc *services.TokenService) *APITokenHandler {
	return &APITokenHandler{tokenSvc: tokenSvc}
}

// tokenExpiry maps the form's choices to durations. Unknown values fall back
// to 90 days rather than never: when the input is garbage, the safer of the
// two defaults is the one that ends.
func tokenExpiry(choice string) *time.Duration {
	switch choice {
	case "never":
		return nil
	case "30d":
		d := 30 * 24 * time.Hour
		return &d
	default:
		d := 90 * 24 * time.Hour
		return &d
	}
}

// CreateToken handles POST /settings/tokens and answers with the whole pane.
// On success the pane carries the plaintext, the only response that ever will:
// the reveal renders from this POST and from nowhere else, so a refresh of
// /settings cannot show it again.
func (h *APITokenHandler) CreateToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := middleware.UserFrom(ctx)

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	plaintext, token, err := h.tokenSvc.Create(ctx, user.ID, r.FormValue("name"), tokenExpiry(r.FormValue("expires")))
	if err != nil {
		// 200 on purpose: htmx drops the body of a non-2xx response, so an
		// error sent as 400 would leave the form looking like nothing
		// happened.
		h.renderPane(w, r, view.TokensData{
			Errors: fieldErrorsOr(err, "name", "Could not create the token. Try again."),
		})
		return
	}

	h.renderPane(w, r, view.TokensData{Revealed: plaintext, RevealedName: token.Name})
}

// RevokeToken handles POST /settings/tokens/{id}/revoke.
func (h *APITokenHandler) RevokeToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := middleware.UserFrom(ctx)

	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err == nil {
		err = h.tokenSvc.Revoke(ctx, user.ID, id)
	}
	if err != nil {
		// Somebody else's token id and a stale row land here together, and
		// the pane re-rendering without the row is the answer either way.
		cslog.L(ctx).WithError(err).Debug("token revoke refused")
	}
	h.renderPane(w, r, view.TokensData{})
}

// renderPane fills the list and writes the pane. Every path ends here so the
// pane never renders without the current list.
func (h *APITokenHandler) renderPane(w http.ResponseWriter, r *http.Request, data view.TokensData) {
	ctx := r.Context()
	user := middleware.UserFrom(ctx)

	tokens, err := h.tokenSvc.ListForUser(ctx, user.ID)
	if err != nil {
		cslog.L(ctx).WithError(err).Error("Could not list tokens")
		if len(data.Errors) == 0 {
			data.Errors = []utils.FieldError{{Field: "name", Detail: "Could not load your tokens. Reload the page."}}
		}
	}
	data.Tokens = tokens
	view.TokensPane(data).Render(ctx, w)
}
