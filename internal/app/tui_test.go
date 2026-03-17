package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"hnet/internal/api"
)

func TestConfigPageAddShortcutClearsInput(t *testing.T) {
	m := model{
		activePage:  pageConfig,
		configFocus: configFocusSubscriptions,
		input:       textinput.New(),
		status: &api.StatusResponse{
			SubscriptionURL: "https://current.example.com/sub",
			Subscriptions: []api.SubscriptionOption{{
				Name: "current.example.com",
				URL:  "https://current.example.com/sub",
			}},
		},
	}
	m.input.SetValue("https://stale.example.com/sub")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	got := updated.(model)

	if got.configFocus != configFocusInput {
		t.Fatalf("expected config focus input, got %v", got.configFocus)
	}
	if got.input.Value() != "" {
		t.Fatalf("expected cleared input, got %q", got.input.Value())
	}
}

func TestConfigPageEditShortcutPrefillsSelectedSubscription(t *testing.T) {
	m := model{
		activePage:         pageConfig,
		configFocus:        configFocusSubscriptions,
		input:              textinput.New(),
		subscriptionCursor: 1,
		status: &api.StatusResponse{
			SubscriptionURL: "https://first.example.com/sub",
			Subscriptions: []api.SubscriptionOption{
				{Name: "first.example.com", URL: "https://first.example.com/sub"},
				{Name: "second.example.com", URL: "https://second.example.com/sub"},
			},
		},
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	got := updated.(model)

	if got.configFocus != configFocusInput {
		t.Fatalf("expected config focus input, got %v", got.configFocus)
	}
	if got.input.Value() != "https://second.example.com/sub" {
		t.Fatalf("expected selected subscription URL, got %q", got.input.Value())
	}
}

func TestRenderConfigActionsIncludesAddAndUpdate(t *testing.T) {
	m := model{}
	view := m.renderConfigActions()
	if view == "" {
		t.Fatal("expected rendered config actions")
	}
	if !containsAll(view, "Add Subscription", "Update Selected") {
		t.Fatalf("unexpected actions view %q", view)
	}
}

func TestSyncFromStatusDoesNotExposeSubscriptionURLInInput(t *testing.T) {
	m := model{
		input: textinput.New(),
		status: &api.StatusResponse{
			SubscriptionURL: "https://hidden.example.com/sub?token=secret",
			Subscriptions: []api.SubscriptionOption{{
				Name: "hidden.example.com",
				URL:  "https://hidden.example.com/sub?token=secret",
			}},
		},
	}

	m.syncFromStatus()

	if got := m.input.Value(); got != "" {
		t.Fatalf("expected blank input after status sync, got %q", got)
	}
}

func TestActionMsgHideURLClearsInputAfterImport(t *testing.T) {
	m := model{
		activePage:  pageConfig,
		configFocus: configFocusInput,
		input:       textinput.New(),
		status: &api.StatusResponse{
			Subscriptions: nil,
		},
	}
	m.input.Focus()
	m.input.SetValue("https://secret.example.com/sub?token=abc")

	updated, _ := m.Update(actionMsg{
		status: &api.StatusResponse{
			SubscriptionURL: "https://secret.example.com/sub?token=abc",
			Subscriptions: []api.SubscriptionOption{{
				Name: "secret.example.com",
				URL:  "https://secret.example.com/sub?token=abc",
			}},
		},
		flash:   "subscription imported and mihomo restarted",
		hideURL: true,
	})
	got := updated.(model)

	if got.input.Value() != "" {
		t.Fatalf("expected input to be cleared, got %q", got.input.Value())
	}
	if got.configFocus != configFocusSubscriptions {
		t.Fatalf("expected config focus subscriptions, got %v", got.configFocus)
	}
}

func TestRenderSubscriptionsDoesNotShowSubscriptionURL(t *testing.T) {
	m := model{
		configFocus: configFocusSubscriptions,
		status: &api.StatusResponse{
			SubscriptionURL: "https://hidden.example.com/sub?token=abc",
			Subscriptions: []api.SubscriptionOption{{
				Name: "hidden.example.com",
				URL:  "https://hidden.example.com/sub?token=abc",
			}},
		},
	}

	view := m.renderSubscriptions()
	if strings.Contains(view, "token=abc") || strings.Contains(view, "https://hidden.example.com") {
		t.Fatalf("expected subscriptions view to hide raw URL, got %q", view)
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
