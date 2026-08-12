package handlers

import (
	"errors"
	"net/http"

	"github.com/getcodescout/code_scout/internal/ports"
	"github.com/getcodescout/code_scout/internal/services"
	"github.com/getcodescout/code_scout/pkg/cslog"
	"github.com/getcodescout/code_scout/pkg/utils"
	"github.com/getcodescout/code_scout/server/middleware"
	"github.com/getcodescout/code_scout/view"
)

type InstanceSettingsHandler struct {
	settingsSvc *services.InstanceSettingsService
	memberSvc   *services.MemberService
	projectSvc  ports.ProjectManager
	versionSvc  *services.VersionService
	tokenSvc    *services.TokenService
}

func NewInstanceSettingsHandler(
	settingsSvc *services.InstanceSettingsService,
	memberSvc *services.MemberService,
	projectSvc ports.ProjectManager,
	versionSvc *services.VersionService,
	tokenSvc *services.TokenService,
) *InstanceSettingsHandler {
	return &InstanceSettingsHandler{
		settingsSvc: settingsSvc,
		memberSvc:   memberSvc,
		projectSvc:  projectSvc,
		versionSvc:  versionSvc,
		tokenSvc:    tokenSvc,
	}
}

// Settings renders GET /settings. Members lives here as a tab rather than on a
// screen of its own, so everything instance-wide is in one place.
func (h *InstanceSettingsHandler) Settings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user := middleware.UserFrom(ctx)
	data := view.InstanceSettingsData{
		User:     user,
		Settings: h.settingsSvc.Current(),
		Tab:      r.URL.Query().Get("tab"),
		Members:  membersData(r, h.memberSvc, h.projectSvc),
		Update:   h.versionSvc.Current(),
	}

	// The GET never carries a plaintext token; only the create POST does.
	if tokens, err := h.tokenSvc.ListForUser(ctx, user.ID); err == nil {
		data.Tokens = view.TokensData{Tokens: tokens}
	} else {
		cslog.L(ctx).WithError(err).Error("Could not list tokens for settings")
	}

	if r.Header.Get("HX-Request") == "true" {
		view.InstanceSettingsBody(data).Render(ctx, w)
		return
	}
	view.InstanceSettingsPage(data).Render(ctx, w)
}

// UpdateDisplay handles POST /settings/display and answers with the whole
// form: saved, or the same values plus an inline error.
func (h *InstanceSettingsHandler) UpdateDisplay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	name := r.FormValue("timezone")
	format := r.FormValue("time_format")

	data := view.InstanceSettingsData{User: middleware.UserFrom(ctx)}

	// Only when the field was actually sent. The form always sends it — a radio
	// group is never unset — but a request without it means "no opinion about
	// the clock", not "set it to empty and fail".
	if _, err := h.settingsSvc.UpdateTimeFormat(ctx, format); r.Form.Has("time_format") && err != nil {
		data.Settings = h.settingsSvc.Current()
		var appErr *utils.ErrorJson
		if errors.As(err, &appErr) && len(appErr.Errors) > 0 {
			data.Errors = appErr.Errors
		} else {
			data.Errors = []utils.FieldError{{Field: "time_format", Detail: "Could not save the clock format. Try again."}}
		}
		view.DisplayForm(data).Render(ctx, w)
		return
	}

	if _, err := h.settingsSvc.UpdateTimezone(ctx, name); err != nil {
		// 200 on purpose: htmx drops the body of a non-2xx response, so an
		// error sent as 400 would leave the form looking like nothing happened.
		data.Settings = h.settingsSvc.Current()
		var appErr *utils.ErrorJson
		if errors.As(err, &appErr) && len(appErr.Errors) > 0 {
			data.Errors = appErr.Errors
		} else {
			data.Errors = []utils.FieldError{{Field: "timezone", Detail: "Could not save the timezone. Try again."}}
		}
		view.DisplayForm(data).Render(ctx, w)
		return
	}

	// The view formats every timestamp through this, so it has to move with the
	// setting or the page would save and still render the old zone.
	settings := h.settingsSvc.Current()
	view.SetTimeZone(settings.Location())
	view.SetTwelveHourClock(settings.TwelveHour())
	cslog.L(ctx).WithField("timezone", settings.Timezone).
		WithField("time_format", settings.TimeFormat).Debug("Render clock updated")

	data.Settings = settings
	data.Saved = true
	view.DisplayForm(data).Render(ctx, w)
}

// UpdateRetention handles POST /settings/retention.
//
// The one deliberate departure from UpdateDisplay: on failure this echoes the
// POSTED values back rather than the stored ones. Re-rendering what is stored
// is right for a select — you cannot select an option that does not exist — and
// wrong for a number, where it silently throws away what the user typed and
// leaves them nothing to correct.
func (h *InstanceSettingsHandler) UpdateRetention(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	keep := r.FormValue("retention_days")
	purge := r.FormValue("purge_after_days")

	data := view.InstanceSettingsData{User: middleware.UserFrom(ctx)}
	if _, err := h.settingsSvc.UpdateRetention(ctx, keep, purge); err != nil {
		// 200 on purpose: htmx drops the body of a non-2xx response.
		data.Settings = h.settingsSvc.Current()
		data.Raw = map[string]string{"retention_days": keep, "purge_after_days": purge}
		data.Errors = fieldErrorsOr(err, "retention_days", "Could not save retention. Try again.")
		view.RetentionForm(data).Render(ctx, w)
		return
	}

	data.Settings = h.settingsSvc.Current()
	data.Saved = true
	view.RetentionForm(data).Render(ctx, w)
}

// UpdateLimits handles POST /settings/limits.
func (h *InstanceSettingsHandler) UpdateLimits(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	maxUpload := r.FormValue("max_upload_mb")
	dailyCap := r.FormValue("daily_log_cap")

	data := view.InstanceSettingsData{User: middleware.UserFrom(ctx)}
	if _, err := h.settingsSvc.UpdateLimits(ctx, maxUpload, dailyCap); err != nil {
		data.Settings = h.settingsSvc.Current()
		data.Raw = map[string]string{"max_upload_mb": maxUpload, "daily_log_cap": dailyCap}
		data.Errors = fieldErrorsOr(err, "max_upload_mb", "Could not save the limits. Try again.")
		view.LimitsForm(data).Render(ctx, w)
		return
	}

	data.Settings = h.settingsSvc.Current()
	data.Saved = true
	view.LimitsForm(data).Render(ctx, w)
}

// ToggleUpdateCheck handles POST /settings/update-check, the checkbox on the
// About card.
//
// An unchecked box sends no field at all, which is how HTML forms represent
// false. So the presence of the key is the value, and reading r.FormValue
// against "on" would work by accident while making the reason invisible.
func (h *InstanceSettingsHandler) ToggleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	enabled := r.Form.Has("enabled")

	if _, err := h.settingsSvc.SetUpdateCheckEnabled(ctx, enabled); err != nil {
		cslog.L(ctx).WithError(err).Error("Could not save the update check setting")
		// 200, and the card re-renders showing what is actually stored, so a
		// failed save leaves the checkbox where the database has it rather than
		// where the click put it.
		h.renderAbout(w, r)
		return
	}

	// Turning it on checks immediately. Waiting until 04:17 tomorrow to answer
	// a question somebody just asked is the kind of thing that reads as broken.
	if enabled {
		view.SetUpdateState(h.versionSvc.Check(ctx))
	}
	h.renderAbout(w, r)
}

// CheckForUpdateNow handles POST /settings/update-check/now.
func (h *InstanceSettingsHandler) CheckForUpdateNow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Check records its own failures on the state it returns, so there is no
	// error path here: an unreachable GitHub renders as a card that says so.
	view.SetUpdateState(h.versionSvc.Check(ctx))
	h.renderAbout(w, r)
}

// renderAbout answers with the card, always 200.
//
// htmx does not swap the body of a non-2xx response, so a handler that reported
// a failed check as 502 would leave the button looking inert. The outcome
// belongs in the HTML.
func (h *InstanceSettingsHandler) renderAbout(w http.ResponseWriter, r *http.Request) {
	view.AboutCard(view.AboutData{
		Update:  h.versionSvc.Current(),
		Enabled: h.settingsSvc.Current().UpdateCheckEnabled,
	}).Render(r.Context(), w)
}

// fieldErrorsOr pulls the inline errors out of a service error, falling back to
// one generic message against the named field so the card is never silent.
func fieldErrorsOr(err error, field, fallback string) []utils.FieldError {
	var appErr *utils.ErrorJson
	if errors.As(err, &appErr) && len(appErr.Errors) > 0 {
		return appErr.Errors
	}
	return []utils.FieldError{{Field: field, Detail: fallback}}
}
