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

func TestConfigPageStartsOnConfigTab(t *testing.T) {
	m := model{activePage: pageConfig}
	view := m.renderPageTabs()
	if !strings.Contains(view, "Config") {
		t.Fatalf("expected config tab to be active, got %q", view)
	}
}

func TestProxyToggleShortcutIsGlobal(t *testing.T) {
	m := model{
		activePage: pageNodes,
		status: &api.StatusResponse{
			SystemProxyEnabled: false,
		},
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	got := updated.(model)

	if !got.busy {
		t.Fatal("expected global proxy toggle to enter busy state")
	}
	if cmd == nil {
		t.Fatal("expected global proxy toggle command")
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

	m.syncFromStatus("", "")

	if got := m.input.Value(); got != "" {
		t.Fatalf("expected blank input after status sync, got %q", got)
	}
}

func TestSyncFromStatusDoesNotAutoOpenEditorWithoutSubscriptions(t *testing.T) {
	m := model{
		activePage:  pageConfig,
		configFocus: configFocusSubscriptions,
		input:       textinput.New(),
		status: &api.StatusResponse{
			Subscriptions: nil,
		},
	}

	m.syncFromStatus("", "")

	if m.configFocus != configFocusSubscriptions {
		t.Fatalf("expected subscriptions focus, got %v", m.configFocus)
	}
	if m.input.Value() != "" {
		t.Fatalf("expected hidden editor input to stay empty, got %q", m.input.Value())
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

func TestRenderConfigPageHidesEditorUntilRequested(t *testing.T) {
	m := model{
		activePage:  pageConfig,
		configFocus: configFocusSubscriptions,
		input:       textinput.New(),
		status: &api.StatusResponse{
			SubscriptionURL: "https://one.example.com/sub",
			Subscriptions: []api.SubscriptionOption{{
				Name: "one.example.com",
				URL:  "https://one.example.com/sub",
			}},
		},
	}

	view := m.renderConfigPage()
	if strings.Contains(view, "Add Subscription") {
		t.Fatalf("expected editor section to be hidden, got %q", view)
	}
}

func TestRenderConfigPageShowsEditorWhenAdding(t *testing.T) {
	m := model{
		activePage:  pageConfig,
		configFocus: configFocusSubscriptions,
		input:       textinput.New(),
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	got := updated.(model)
	view := got.renderConfigPage()

	if !strings.Contains(view, "Add Subscription") {
		t.Fatalf("expected add editor section, got %q", view)
	}
}

func TestRenderConfigPageDoesNotShowShellSourceHint(t *testing.T) {
	m := model{
		activePage:  pageConfig,
		configFocus: configFocusSubscriptions,
		input:       textinput.New(),
		status:      &api.StatusResponse{},
	}

	view := m.renderConfigPage()
	if strings.Contains(view, "shell: source") {
		t.Fatalf("expected shell env source hint to be removed, got %q", view)
	}
}

func TestConfigPageEnterOnActiveSubscriptionDoesNotStartWork(t *testing.T) {
	m := model{
		activePage:  pageConfig,
		configFocus: configFocusSubscriptions,
		input:       textinput.New(),
		status: &api.StatusResponse{
			SubscriptionURL: "https://one.example.com/sub",
			Subscriptions: []api.SubscriptionOption{{
				Name: "one.example.com",
				URL:  "https://one.example.com/sub",
			}},
		},
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)

	if got.busy {
		t.Fatal("expected active subscription enter to be a no-op")
	}
	if cmd != nil {
		t.Fatal("expected no command for active subscription enter")
	}
}

func TestConfigPageRefreshRejectsInactiveSubscription(t *testing.T) {
	m := model{
		activePage:         pageConfig,
		configFocus:        configFocusSubscriptions,
		input:              textinput.New(),
		subscriptionCursor: 1,
		status: &api.StatusResponse{
			SubscriptionURL: "https://one.example.com/sub",
			Subscriptions: []api.SubscriptionOption{
				{Name: "one.example.com", URL: "https://one.example.com/sub"},
				{Name: "two.example.com", URL: "https://two.example.com/sub"},
			},
		},
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	got := updated.(model)

	if cmd != nil {
		t.Fatal("expected no refresh command for inactive subscription")
	}
	if got.err == "" {
		t.Fatal("expected error when refreshing inactive subscription")
	}
}

func TestConfigPageDeleteRequiresConfirmation(t *testing.T) {
	m := model{
		activePage:  pageConfig,
		configFocus: configFocusSubscriptions,
		input:       textinput.New(),
		status: &api.StatusResponse{
			SubscriptionURL: "https://one.example.com/sub",
			Subscriptions: []api.SubscriptionOption{{
				Name: "one.example.com",
				URL:  "https://one.example.com/sub",
			}},
		},
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	got := updated.(model)

	if cmd != nil {
		t.Fatal("expected delete to wait for confirmation")
	}
	if got.deleteConfirmURL != "https://one.example.com/sub" {
		t.Fatalf("expected delete confirmation target to be set, got %q", got.deleteConfirmURL)
	}
}

func TestStatusUpdateKeepsSelectedProxyCursor(t *testing.T) {
	m := model{
		activePage: pageNodes,
		cursor:     1,
		status: &api.StatusResponse{
			CurrentProxy: "node-a",
			AvailableProxies: []api.ProxyOption{
				{Name: "node-a", Alive: true},
				{Name: "node-b", Alive: true},
				{Name: "node-c", Alive: true},
			},
		},
	}

	updated, _ := m.Update(statusMsg{
		status: &api.StatusResponse{
			CurrentProxy: "node-a",
			AvailableProxies: []api.ProxyOption{
				{Name: "node-a", Alive: true},
				{Name: "node-b", Alive: true, LatencyMS: 20},
				{Name: "node-c", Alive: true},
			},
		},
	})
	got := updated.(model)

	if got.cursor != 1 {
		t.Fatalf("expected cursor to stay on selected proxy, got %d", got.cursor)
	}
}

func TestSyncFromStatusFallsBackToActiveSubscriptionWhenSelectionDisappears(t *testing.T) {
	m := model{
		subscriptionCursor: 1,
		status: &api.StatusResponse{
			SubscriptionURL: "https://one.example.com/sub",
			Subscriptions: []api.SubscriptionOption{
				{Name: "one.example.com", URL: "https://one.example.com/sub"},
				{Name: "two.example.com", URL: "https://two.example.com/sub"},
			},
		},
	}

	m.status = &api.StatusResponse{
		SubscriptionURL: "https://one.example.com/sub",
		Subscriptions: []api.SubscriptionOption{
			{Name: "one.example.com", URL: "https://one.example.com/sub"},
			{Name: "three.example.com", URL: "https://three.example.com/sub"},
		},
	}
	m.syncFromStatus("", "https://two.example.com/sub")

	if m.subscriptionCursor != 0 {
		t.Fatalf("expected cursor to fall back to active subscription, got %d", m.subscriptionCursor)
	}
}

func TestSyncFromStatusKeepsSelectedSubscriptionAcrossURLRenameWhenIDMatches(t *testing.T) {
	m := model{
		subscriptionCursor: 1,
		status: &api.StatusResponse{
			SubscriptionURL: "https://one.example.com/sub",
			Subscriptions: []api.SubscriptionOption{
				{ID: "sub_one", Name: "one.example.com", URL: "https://one.example.com/sub"},
				{ID: "sub_two", Name: "two.example.com", URL: "https://old.example.com/sub"},
			},
		},
	}

	m.status = &api.StatusResponse{
		SubscriptionURL: "https://one.example.com/sub",
		Subscriptions: []api.SubscriptionOption{
			{ID: "sub_one", Name: "one.example.com", URL: "https://one.example.com/sub"},
			{ID: "sub_two", Name: "two.example.com", URL: "https://new.example.com/sub"},
		},
	}
	m.syncFromStatus("", "sub_two")

	if m.subscriptionCursor != 1 {
		t.Fatalf("expected cursor to stay on renamed subscription, got %d", m.subscriptionCursor)
	}
}
