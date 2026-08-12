package view

import (
	"strings"
	"testing"

	"github.com/getcodescout/code_scout/internal/domain"
	"github.com/getcodescout/code_scout/pkg/utils"
)

func accountData(role domain.Role, tab string) AccountData {
	return AccountData{
		User: &domain.User{Name: "Test", Email: "t@test.local", Role: role},
		Tab:  tab,
	}
}

// The whole point of the screen: every role gets the same two tabs, because
// everything on it belongs to the signed-in user alone.
func TestAccountTabsAreTheSameForEveryRole(t *testing.T) {
	for _, role := range []domain.Role{domain.RoleSuperAdmin, domain.RoleAdmin, domain.RoleMember} {
		var got []string
		for _, tab := range accountData(role, "").tabs() {
			got = append(got, tab.Key)
		}
		if strings.Join(got, ",") != "tokens,password" {
			t.Errorf("%s should see [tokens password], got %v", role, got)
		}
	}
}

func TestAccountDefaultsToTheTokensTab(t *testing.T) {
	out := render(t, AccountBody(accountData(domain.RoleMember, "")))
	if !strings.Contains(out, "tokens-pane") {
		t.Error("the default tab should be API tokens")
	}
	if strings.Contains(out, `name="current_password"`) {
		t.Error("the password form rendered on the tokens tab")
	}
}

func TestAccountPasswordTabCarriesTheVoluntaryForm(t *testing.T) {
	out := render(t, AccountBody(accountData(domain.RoleMember, "password")))
	// The form posts to the shared endpoint and marks where it came from, so
	// errors and the success redirect return here rather than to the
	// standalone forced-change page.
	if !strings.Contains(out, `action="/change-password"`) {
		t.Error("the password form should post to /change-password")
	}
	if !strings.Contains(out, `name="from" value="account"`) {
		t.Error("the form should say it came from the account page")
	}
	for _, field := range []string{"current_password", "password", "confirm_password"} {
		if !strings.Contains(out, `name="`+field+`"`) {
			t.Errorf("missing field %s", field)
		}
	}
}

func TestAccountPasswordErrorsRenderInline(t *testing.T) {
	d := accountData(domain.RoleMember, "password")
	d.PasswordErrors = []utils.FieldError{{Field: "current_password", Detail: "That is not your current password"}}
	out := render(t, AccountBody(d))
	if !strings.Contains(out, "That is not your current password") {
		t.Error("a refused change should render its error inline")
	}
}
