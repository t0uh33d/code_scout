package view

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/google/uuid"
	"github.com/t0uh33d/code_scout/pkg/utils"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	if err := c.Render(context.Background(), &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

func TestFieldErrorFor(t *testing.T) {
	errs := []utils.FieldError{
		{Field: "name", Message: "generic", Detail: "A project with that name already exists"},
		{Field: "other", Message: "fallback"},
	}

	if got := fieldErrorFor(errs, "name"); got != "A project with that name already exists" {
		t.Errorf("detail should win over message, got %q", got)
	}
	if got := fieldErrorFor(errs, "other"); got != "fallback" {
		t.Errorf("message should be the fallback, got %q", got)
	}
	if got := fieldErrorFor(errs, "missing"); got != "" {
		t.Errorf("unknown field should be empty, got %q", got)
	}
	if got := fieldErrorFor(nil, "name"); got != "" {
		t.Errorf("no errors should be empty, got %q", got)
	}
}

// TestMaskedSecretFieldCarriesNoSecret is the guard against a refactor quietly
// putting the plaintext key back into the delivered page. The masked control is
// what ships with the settings screen; the real value only ever arrives in the
// response to an explicit reveal.
func TestMaskedSecretFieldCarriesNoSecret(t *testing.T) {
	const secret = "SUPERSECRETKEYVALUE1234567890abc"
	out := render(t, SecretField(uuid.New(), secret, false))

	if strings.Contains(out, secret) {
		t.Fatal("the masked secret field leaked the secret into the markup")
	}
	if !strings.Contains(out, "••••") {
		t.Error("expected the masked placeholder")
	}
	if !strings.Contains(out, "Reveal") {
		t.Error("expected a Reveal control")
	}
	if strings.Contains(out, "Copy") {
		t.Error("nothing to copy while masked, so no Copy button")
	}
}

func TestRevealedSecretFieldShowsTheKeyAndHide(t *testing.T) {
	const secret = "SUPERSECRETKEYVALUE1234567890abc"
	out := render(t, SecretField(uuid.New(), secret, true))

	if !strings.Contains(out, secret) {
		t.Fatal("the revealed field should carry the secret")
	}
	if !strings.Contains(out, "Hide") {
		t.Error("revealed field should offer Hide")
	}
	if !strings.Contains(out, "hide=1") {
		t.Error("Hide should point at the masking endpoint")
	}
}

// TestConfirmModalEscapesPhrase locks in reading the phrase from a data
// attribute rather than interpolating it into the script. A project name is
// user-supplied, so it is hostile input.
func TestConfirmModalEscapesPhrase(t *testing.T) {
	hostile := `<img src=x onerror=alert(1)>`
	out := render(t, ConfirmModal(ConfirmDialog{
		Title: "Delete this project", Phrase: hostile,
		ConfirmText: "Delete permanently", PostURL: "/project/x/settings/delete",
		Target: "#confirm-error", Swap: "innerHTML",
	}))

	if strings.Contains(out, "<img src=x") {
		t.Fatal("the phrase was rendered as raw markup")
	}
	if !strings.Contains(out, "&lt;img") {
		t.Error("expected the phrase to appear escaped")
	}
	if !strings.Contains(out, "data-confirm-phrase") {
		t.Error("the phrase should travel as a data attribute, not inside the script")
	}
}

func TestConfirmModalShipsDisabled(t *testing.T) {
	out := render(t, ConfirmModal(ConfirmDialog{
		Title: "Rotate", Phrase: "rotate", ConfirmText: "Rotate secret",
		PostURL: "/project/x/settings/secret/rotate", Target: "#secret-field", Swap: "outerHTML",
	}))

	// Server-side disabled, so the action is safe even if the script fails.
	if !strings.Contains(out, "disabled") {
		t.Error("the confirm button must ship disabled")
	}
	if !strings.Contains(out, `hx-post="/project/x/settings/secret/rotate"`) {
		t.Error("confirm button should post to the action URL")
	}
}

func TestGeneralFormShowsInlineNameError(t *testing.T) {
	out := render(t, GeneralForm(GeneralFormData{
		ProjectID: uuid.New(), Name: "taken",
		Errors: []utils.FieldError{{Field: "name", Detail: "A project with that name already exists"}},
	}))

	if !strings.Contains(out, "A project with that name already exists") {
		t.Fatal("the field error should render")
	}
	if !strings.Contains(out, "border-cs-danger") {
		t.Error("the offending input should be marked")
	}
	// The typed value survives a rejected save.
	if !strings.Contains(out, `value="taken"`) {
		t.Error("the rejected value should stay in the input")
	}
}

func TestGeneralFormSavedState(t *testing.T) {
	out := render(t, GeneralForm(GeneralFormData{
		ProjectID: uuid.New(), Name: "ok", Description: "d", Saved: true,
	}))

	if !strings.Contains(out, "Saved") {
		t.Error("expected the saved acknowledgement")
	}
	if strings.Contains(out, "border-cs-danger") {
		t.Error("a clean save should not mark the input")
	}
}
