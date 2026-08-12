package view

import (
	"strings"
	"testing"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/pkg/utils"
)

func dataFor(role domain.Role, tab string) InstanceSettingsData {
	return InstanceSettingsData{
		User:     &domain.User{Name: "Test", Email: "t@test.local", Role: role},
		Settings: domain.InstanceSettings{Timezone: "UTC"},
		Tab:      tab,
	}
}

// General changes how every project renders, so only the super admin gets it.
// Everyone signed in may see who else exists, and everyone gets API tokens,
// because a token is per user and only ever grants what that user already has.
func TestInstanceSettingsTabsByRole(t *testing.T) {
	cases := []struct {
		role domain.Role
		want []string
	}{
		{domain.RoleSuperAdmin, []string{"general", "members", "tokens"}},
		{domain.RoleAdmin, []string{"members", "tokens"}},
		{domain.RoleMember, []string{"members", "tokens"}},
	}
	for _, c := range cases {
		t.Run(string(c.role), func(t *testing.T) {
			var got []string
			for _, tab := range dataFor(c.role, "").tabs() {
				got = append(got, tab.Key)
			}
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Errorf("%s should see %v, got %v", c.role, c.want, got)
			}
		})
	}
}

// A hand-typed ?tab=general must not render the timezone form for someone who
// may not change it. Hiding the tab is not enough on its own.
func TestInstanceSettingsForbiddenTabFallsBack(t *testing.T) {
	admin := dataFor(domain.RoleAdmin, "general")
	if got := admin.activeTab(); got != "members" {
		t.Errorf("an admin asking for general should land on members, got %q", got)
	}

	out := render(t, InstanceSettingsBody(admin))
	if strings.Contains(out, `name="timezone"`) {
		t.Error("the timezone control rendered for an admin")
	}
	if strings.Contains(out, "/settings/timezone") {
		t.Error("the timezone endpoint was exposed to an admin")
	}
}

func TestInstanceSettingsDefaultsToFirstAvailableTab(t *testing.T) {
	if got := dataFor(domain.RoleSuperAdmin, "").activeTab(); got != "general" {
		t.Errorf("a super admin should land on general, got %q", got)
	}
	if got := dataFor(domain.RoleMember, "").activeTab(); got != "members" {
		t.Errorf("a member should land on members, got %q", got)
	}
	// Nonsense must not panic or render an empty body.
	if got := dataFor(domain.RoleSuperAdmin, "../../etc").activeTab(); got != "general" {
		t.Errorf("an unknown tab should fall back to general, got %q", got)
	}
}

// The tab links have to be real addresses, because the handler serves the same
// URL as a full page for a browser and as a fragment for htmx.
func TestInstanceSettingsTabsLinkToRealAddresses(t *testing.T) {
	out := render(t, InstanceSettingsBody(dataFor(domain.RoleSuperAdmin, "general")))
	for _, want := range []string{`hx-get="/settings?tab=general"`, `hx-get="/settings?tab=members"`, `hx-push-url="true"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s", want)
		}
	}
	if !strings.Contains(out, `hx-target="#instance-settings-body"`) {
		t.Error("tabs must swap the body that holds them, or the underline never moves")
	}
}

// Each card is its own form posting to its own endpoint and replacing only
// itself. If two shared a target, saving one would blank the other.
func TestSettingsCardsSelfTarget(t *testing.T) {
	d := InstanceSettingsData{Settings: domain.DefaultInstanceSettings()}

	for _, c := range []struct {
		name   string
		markup string
		id     string
		post   string
	}{
		{"retention", render(t, RetentionForm(d)), "retention-form", "/settings/retention"},
		{"limits", render(t, LimitsForm(d)), "limits-form", "/settings/limits"},
	} {
		if !contains(c.markup, `id="`+c.id+`"`) {
			t.Errorf("%s lost its id: %s", c.name, c.markup)
		}
		if !contains(c.markup, `hx-target="#`+c.id+`"`) {
			t.Errorf("%s does not target itself: %s", c.name, c.markup)
		}
		if !contains(c.markup, `hx-post="`+c.post+`"`) {
			t.Errorf("%s posts somewhere else: %s", c.name, c.markup)
		}
	}
}

// The prototype's labels, word for word. The cards are the spec.
func TestSettingsCardsUsePrototypeLabels(t *testing.T) {
	d := InstanceSettingsData{Settings: domain.DefaultInstanceSettings()}

	retention := render(t, RetentionForm(d))
	for _, want := range []string{"Retention", "Keep logs for", "Purge deleted after"} {
		if !contains(retention, want) {
			t.Errorf("retention card is missing %q", want)
		}
	}

	limits := render(t, LimitsForm(d))
	for _, want := range []string{"Limits", "Max upload size"} {
		if !contains(limits, want) {
			t.Errorf("limits card is missing %q", want)
		}
	}
}

// The stored values render in the units a person types, not in bytes.
func TestSettingsCardsRenderStoredValues(t *testing.T) {
	d := InstanceSettingsData{Settings: domain.InstanceSettings{
		RetentionDays: 90, PurgeAfterDays: 14, MaxUploadBytes: 25 << 20,
	}}

	retention := render(t, RetentionForm(d))
	if !contains(retention, `value="90"`) || !contains(retention, `value="14"`) {
		t.Errorf("stored retention values are missing: %s", retention)
	}

	limits := render(t, LimitsForm(d))
	if !contains(limits, `value="25"`) {
		t.Errorf("want 25 MB rather than a byte count: %s", limits)
	}
	if contains(limits, "26214400") {
		t.Errorf("nobody types a byte count: %s", limits)
	}
}

// A refused number must stay on screen to be corrected. This is the one place
// these cards deliberately differ from the timezone select, which re-renders
// the stored value because a select cannot hold one it does not offer.
func TestRefusedValueIsEchoedBack(t *testing.T) {
	d := InstanceSettingsData{
		Settings: domain.InstanceSettings{RetentionDays: 30, PurgeAfterDays: 7},
		Raw:      map[string]string{"retention_days": "9999999", "purge_after_days": "7"},
		Errors:   []utils.FieldError{{Field: "retention_days", Detail: "Enter a whole number of days between 1 and 3650."}},
	}

	out := render(t, RetentionForm(d))
	if !contains(out, `value="9999999"`) {
		t.Errorf("the rejected value was thrown away, leaving nothing to correct: %s", out)
	}
	if !contains(out, "between 1 and 3650") {
		t.Errorf("the inline error is missing: %s", out)
	}
	if !contains(out, "border-cs-danger") {
		t.Errorf("the field is not marked as in error: %s", out)
	}
}

// An admin never sees the General pane, so the endpoints that change the whole
// instance must not appear in their markup either.
func TestInstanceSettingsHidesLimitsFromAdmins(t *testing.T) {
	admin := &domain.User{Role: domain.RoleAdmin}
	out := render(t, InstanceSettingsBody(InstanceSettingsData{
		User: admin, Settings: domain.DefaultInstanceSettings(), Tab: "general",
	}))

	for _, forbidden := range []string{"/settings/retention", "/settings/limits", `name="retention_days"`, `name="max_upload_mb"`} {
		if contains(out, forbidden) {
			t.Errorf("an admin can see %q", forbidden)
		}
	}
}
