package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"socprint/internal/catalog"
	"socprint/internal/config"
	"socprint/internal/transport"
)

func TestFirstLaunchGuidesSignInStepByStep(t *testing.T) {
	printers, err := catalog.All()
	if err != nil {
		t.Fatal(err)
	}
	model := New(config.Settings{}, printers, nil)
	model.width, model.height = 100, 30
	initial := model.View()
	for _, text := range []string{"Sign in · Step 1 of 2", "Account: Not set", "Username", "Create a SoC account", "Enable Unix access"} {
		if !strings.Contains(initial, text) {
			t.Errorf("initial screen is missing %q", text)
		}
	}
	for _, hidden := range []string{"[Print]", "Printers", "Jobs", "Queues", "Help", "Password", "SSH key", "U to edit", "Type U"} {
		if strings.Contains(initial, hidden) {
			t.Errorf("first sign-in screen shows unrelated detail %q", hidden)
		}
	}
	for _, removed := range []string{"welcome", "demo only", "v0.1", "Keep it simple.", "Type U to edit"} {
		if strings.Contains(strings.ToLower(initial), strings.ToLower(removed)) {
			t.Errorf("initial screen still contains %q", removed)
		}
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("supan")})
	model = updated.(*Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(*Model)
	if model.accountStep != accountPassword || model.editField != "password" {
		t.Fatalf("username step did not advance to password: step=%d field=%q", model.accountStep, model.editField)
	}
	passwordView := model.View()
	if !strings.Contains(passwordView, "Sign in · Step 2 of 2") || !strings.Contains(passwordView, "Password") {
		t.Fatalf("password step was not shown: %s", passwordView)
	}
	if strings.Contains(passwordView, "SSH key") || strings.Contains(passwordView, "Printers") {
		t.Fatal("password step should not expose SSH setup or app navigation")
	}
}

func TestKeyboardTabNavigationAfterSignIn(t *testing.T) {
	printers, err := catalog.All()
	if err != nil {
		t.Fatal(err)
	}
	model := New(config.Settings{Username: "supan"}, printers, nil)
	model.page = "Print"
	model.connected = true
	model.editField = ""
	model.width, model.height = 100, 30
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if updated.(*Model).page != "Printers" {
		t.Fatalf("Tab should navigate between app sections after sign-in, got %q", updated.(*Model).page)
	}
}

func TestAccountHeaderClickOpensCredentials(t *testing.T) {
	printers, err := catalog.All()
	if err != nil {
		t.Fatal(err)
	}
	model := New(config.Settings{}, printers, nil)
	model.page = "Print"
	model.editField = "filename"
	model.width, model.height = 100, 30
	model.View()
	updated, _ := model.Update(tea.MouseMsg{X: 5, Y: 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if updated.(*Model).page != "Account" {
		t.Fatal("clicking the account status should open account settings")
	}
}

func TestSignInButtonIsMouseClickable(t *testing.T) {
	printers, err := catalog.All()
	if err != nil {
		t.Fatal(err)
	}
	model := New(config.Settings{Username: "supan"}, printers, nil)
	model.page = "Account"
	model.accountStep = accountPassword
	model.editField = "password"
	model.input = []rune("fixture-password")
	model.width, model.height = 100, 30
	model.View()
	for _, area := range model.zones {
		if area.action == "account-submit" {
			updated, command := model.Update(tea.MouseMsg{X: 5, Y: area.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			if command == nil || !updated.(*Model).loginPending {
				t.Fatal("clicking Sign in should start the connection")
			}
			return
		}
	}
	t.Fatal("Sign in button is missing from the password step")
}

func TestTrustPromptIsFocusedAndShowsServerIdentity(t *testing.T) {
	printers, err := catalog.All()
	if err != nil {
		t.Fatal(err)
	}
	model := New(config.Settings{Username: "supan"}, printers, nil)
	model.width, model.height = 100, 30
	model.trust = &transport.TrustRequired{Host: "sjump.comp.nus.edu.sg:22", Fingerprint: "SHA256:fixture"}
	view := model.View()
	for _, text := range []string{"Verify the server before signing in", "sjump.comp.nus.edu.sg:22", "SHA256:fixture", "I verified it — continue"} {
		if !strings.Contains(view, text) {
			t.Errorf("server verification screen is missing %q", text)
		}
	}
	for _, hidden := range []string{"Printers", "Jobs", "SSH key needed", "Username:"} {
		if strings.Contains(view, hidden) {
			t.Errorf("server verification screen exposes unrelated detail %q", hidden)
		}
	}
}

func TestNoKeyErrorOpensOnlyTheRequiredKeySetupStep(t *testing.T) {
	printers, err := catalog.All()
	if err != nil {
		t.Fatal(err)
	}
	model := New(config.Settings{Username: "supan"}, printers, nil)
	model.handleLoginError(transport.ErrNoKey)
	view := model.View()
	if !strings.Contains(view, "SSH key needed") || !strings.Contains(view, "Create a secure SSH key") {
		t.Fatal("jump-host login failure should explain the required SSH-key step")
	}
	for _, hidden := range []string{"U to edit", "Password:", "Forget saved", "New passphrase"} {
		if strings.Contains(view, hidden) {
			t.Errorf("key setup step exposes unrelated detail %q", hidden)
		}
	}
}

func TestAuthenticationFailureReturnsToPasswordStep(t *testing.T) {
	printers, err := catalog.All()
	if err != nil {
		t.Fatal(err)
	}
	model := New(config.Settings{Username: "supan"}, printers, nil)
	model.handleLoginError(transport.ErrAuthentication)
	view := model.View()
	if model.accountStep != accountPassword || !strings.Contains(view, "username or password was not accepted") {
		t.Fatalf("authentication failure should guide the user back to password entry: %s", view)
	}
	if strings.Contains(view, "ssh: handshake failed") || strings.Contains(view, "U to edit") {
		t.Fatal("technical details leaked into the sign-in retry step")
	}
}

func TestPrinterListScrollKeepsSelectionVisibleAfterResize(t *testing.T) {
	printers, err := catalog.All()
	if err != nil {
		t.Fatal(err)
	}
	model := New(config.Settings{}, printers, nil)
	model.page = "Printers"
	model.selectedTab = 2
	model.width, model.height = 80, 12
	model.View()
	for index := 0; index < 10; index++ {
		updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = updated.(*Model)
		current := model.visiblePrinters[model.selected].ID
		if view := model.View(); !strings.Contains(view, current) {
			t.Fatalf("selected printer %s is not visible after scroll", current)
		}
	}
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	model = updated.(*Model)
	current := model.visiblePrinters[model.selected].ID
	if view := model.View(); !strings.Contains(view, current) {
		t.Fatalf("selected printer %s is not visible after terminal resize", current)
	}
}

func TestUnknownOutcomeBlocksAccidentalRepeatConfirmation(t *testing.T) {
	printers, err := catalog.All()
	if err != nil {
		t.Fatal(err)
	}
	model := New(config.Settings{Username: "student"}, printers, nil)
	model.page = "Print"
	model.selectedTab = 1
	model.width, model.height = 100, 30
	model.submissionUnknown = true
	model.unknownOperation = "0123456789abcdef0123456789abcdef"
	model.fileName = "notes.pdf"
	model.queue = "psc008-sx"
	model.editField = ""
	view := model.View()
	if !strings.Contains(view, "Outcome unknown") || !strings.Contains(view, "Inspect this printer queue") {
		t.Fatal("unknown submission screen must tell the user to inspect the queue")
	}
	if strings.Contains(view, "Confirm print") {
		t.Fatal("unknown submission screen must not expose the normal confirmation action")
	}
}
