package view

import (
	"strings"
	"testing"

	"github.com/t0uh33d/code_scout/internal/domain"
)

func dataFor(role domain.Role, tab string) InstanceSettingsData {
	return InstanceSettingsData{
		User:     &domain.User{Name: "Test", Email: "t@test.local", Role: role},
		Settings: domain.InstanceSettings{Timezone: "UTC"},
		Tab:      tab,
	}
}

// General changes how every project renders, so only the super admin gets it.
// Everyone signed in may see who else exists.
func TestInstanceSettingsTabsByRole(t *testing.T) {
	cases := []struct {
		role domain.Role
		want []string
	}{
		{domain.RoleSuperAdmin, []string{"general", "members"}},
		{domain.RoleAdmin, []string{"members"}},
		{domain.RoleMember, []string{"members"}},
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
