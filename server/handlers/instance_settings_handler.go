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
}

func NewInstanceSettingsHandler(
	settingsSvc *services.InstanceSettingsService,
	memberSvc *services.MemberService,
	projectSvc ports.ProjectManager,
) *InstanceSettingsHandler {
	return &InstanceSettingsHandler{settingsSvc: settingsSvc, memberSvc: memberSvc, projectSvc: projectSvc}
}

// Settings renders GET /settings. Members lives here as a tab rather than on a
// screen of its own, so everything instance-wide is in one place.
func (h *InstanceSettingsHandler) Settings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	data := view.InstanceSettingsData{
		User:     middleware.UserFrom(ctx),
		Settings: h.settingsSvc.Current(),
		Tab:      r.URL.Query().Get("tab"),
		Members:  membersData(r, h.memberSvc, h.projectSvc),
	}

	if r.Header.Get("HX-Request") == "true" {
		view.InstanceSettingsBody(data).Render(ctx, w)
		return
	}
	view.InstanceSettingsPage(data).Render(ctx, w)
}

// UpdateTimezone handles POST /settings/timezone and answers with the whole
// form: saved, or the same values plus an inline error.
func (h *InstanceSettingsHandler) UpdateTimezone(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	name := r.FormValue("timezone")

	data := view.InstanceSettingsData{User: middleware.UserFrom(ctx)}
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
		view.TimezoneForm(data).Render(ctx, w)
		return
	}

	// The view formats every timestamp through this, so it has to move with the
	// setting or the page would save and still render the old zone.
	settings := h.settingsSvc.Current()
	view.SetTimeZone(settings.Location())
	cslog.L(ctx).WithField("timezone", settings.Timezone).Debug("Render timezone updated")

	data.Settings = settings
	data.Saved = true
	view.TimezoneForm(data).Render(ctx, w)
}

// UpdateRetention handles POST /settings/retention.
//
// The one deliberate departure from UpdateTimezone: on failure this echoes the
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

// fieldErrorsOr pulls the inline errors out of a service error, falling back to
// one generic message against the named field so the card is never silent.
func fieldErrorsOr(err error, field, fallback string) []utils.FieldError {
	var appErr *utils.ErrorJson
	if errors.As(err, &appErr) && len(appErr.Errors) > 0 {
		return appErr.Errors
	}
	return []utils.FieldError{{Field: field, Detail: fallback}}
}
