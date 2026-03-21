package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"hnet/internal/api"
	"hnet/internal/client"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	labelStyle    = lipgloss.NewStyle().Bold(true)
	hintStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	okStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	focusStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Bold(true)
	currentStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	disabledStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

type page int

const (
	pageNodes page = iota
	pageConfig
)

type configFocus int

const (
	configFocusSubscriptions configFocus = iota
	configFocusInput
)

type statusMsg struct {
	status *api.StatusResponse
	err    error
}

type actionMsg struct {
	status  *api.StatusResponse
	err     error
	flash   string
	hideURL bool
}

type pollStatusMsg struct{}

type model struct {
	client             *client.Client
	paths              Paths
	input              textinput.Model
	deleteConfirmURL   string
	status             *api.StatusResponse
	busy               bool
	flash              string
	err                string
	width              int
	height             int
	activePage         page
	cursor             int
	configFocus        configFocus
	subscriptionCursor int
}

func RunTUI(cli *client.Client, paths Paths) error {
	input := textinput.New()
	input.Placeholder = "Paste a subscription URL and press Ctrl+S"
	input.Prompt = "> "
	input.CharLimit = 2048
	input.Width = 72
	input.Blur()

	m := model{
		client:     cli,
		paths:      paths,
		input:      input,
		activePage: pageConfig,
	}

	program := tea.NewProgram(m)
	_, err := program.Run()
	return err
}

func (m model) Init() tea.Cmd {
	return m.fetchStatusCmd()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case statusMsg:
		selectedProxyID := m.selectedProxyID()
		selectedSubscriptionID := m.selectedSubscriptionID()
		m.busy = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.err = ""
		m.status = msg.status
		m.syncFromStatus(selectedProxyID, selectedSubscriptionID)
		if m.subscriptionOpRunning() {
			return m, pollStatusCmd()
		}
	case actionMsg:
		selectedProxyID := m.selectedProxyID()
		selectedSubscriptionID := m.selectedSubscriptionID()
		m.busy = false
		if msg.err != nil {
			m.err = msg.err.Error()
			m.flash = ""
			return m, nil
		}
		m.err = ""
		m.status = msg.status
		m.syncFromStatus(selectedProxyID, selectedSubscriptionID)
		if m.subscriptionOpRunning() {
			m.flash = "subscription operation started"
			if msg.hideURL {
				m.finishSubscriptionInput()
			}
			return m, pollStatusCmd()
		}
		m.flash = msg.flash
		if msg.hideURL {
			m.finishSubscriptionInput()
		}
	case pollStatusMsg:
		if m.subscriptionOpRunning() {
			return m, m.fetchStatusCmd()
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "r":
			m.busy = true
			m.flash = ""
			m.err = ""
			return m, m.fetchStatusCmd()
		case "tab":
			m.togglePage()
			return m, nil
		case "p":
			if m.status == nil {
				m.err = "hnetd is not reachable"
				return m, nil
			}
			if m.subscriptionOpRunning() {
				m.err = "subscription operation in progress"
				return m, nil
			}
			m.busy = true
			m.flash = ""
			m.err = ""
			enabled := !m.status.SystemProxyEnabled
			flash := "system proxy disabled"
			if enabled {
				flash = "system proxy enabled"
			}
			return m, m.systemProxyCmd(enabled, flash)
		}

		if m.activePage == pageNodes {
			if m.subscriptionOpRunning() {
				switch msg.String() {
				case "up", "k":
					m.moveCursor(-1)
				case "down", "j":
					m.moveCursor(1)
				case "enter", "t":
					m.err = "wait for the subscription operation to finish"
				}
				return m, nil
			}
			switch msg.String() {
			case "up", "k":
				m.moveCursor(-1)
				return m, nil
			case "down", "j":
				m.moveCursor(1)
				return m, nil
			case "enter":
				if proxy, ok := m.selectedProxy(); ok {
					m.busy = true
					m.flash = ""
					m.err = ""
					return m, m.selectProxyCmd(proxy.Name)
				}
			case "t":
				if proxy, ok := m.selectedProxy(); ok {
					m.busy = true
					m.flash = ""
					m.err = ""
					return m, m.testProxyCmd(proxy.Name)
				}
			}
			return m, nil
		}

		if m.configFocus == configFocusSubscriptions {
			if m.deleteConfirmURL != "" {
				switch msg.String() {
				case "esc":
					m.deleteConfirmURL = ""
					return m, nil
				case "enter":
					m.busy = true
					m.flash = ""
					m.err = ""
					targetURL := m.deleteConfirmURL
					m.deleteConfirmURL = ""
					return m, m.deleteSubscriptionCmd(targetURL)
				}
				return m, nil
			}

			switch msg.String() {
			case "a":
				if m.subscriptionOpRunning() {
					m.err = "wait for the subscription operation to finish"
					return m, nil
				}
				m.focusSubscriptionInput("")
				return m, nil
			case "u":
				if m.subscriptionOpRunning() {
					m.err = "wait for the subscription operation to finish"
					return m, nil
				}
				if subscription, ok := m.selectedSubscription(); ok {
					if m.status == nil || subscription.URL != m.status.SubscriptionURL {
						m.err = "refresh only works for the active subscription"
						return m, nil
					}
					m.busy = true
					m.flash = ""
					m.err = ""
					return m, m.refreshSubscriptionCmd(subscription.URL)
				}
				return m, nil
			case "d":
				if m.subscriptionOpRunning() {
					m.err = "wait for the subscription operation to finish"
					return m, nil
				}
				if subscription, ok := m.selectedSubscription(); ok {
					m.deleteConfirmURL = subscription.URL
					m.flash = ""
					m.err = ""
				}
				return m, nil
			case "up", "k":
				m.moveSubscriptionCursor(-1)
				return m, nil
			case "down", "j":
				m.moveSubscriptionCursor(1)
				return m, nil
			case "enter":
				if subscription, ok := m.selectedSubscription(); ok {
					if m.subscriptionOpRunning() {
						m.err = "wait for the subscription operation to finish"
						return m, nil
					}
					if m.status != nil && subscription.URL == m.status.SubscriptionURL {
						m.flash = "subscription already active"
						m.err = ""
						return m, nil
					}
					m.busy = true
					m.flash = ""
					m.err = ""
					return m, m.selectSubscriptionCmd(subscription.URL)
				}
			}
			return m, nil
		}

		switch msg.String() {
		case "esc":
			m.finishSubscriptionInput()
			return m, nil
		case "enter", "ctrl+s":
			value := strings.TrimSpace(m.input.Value())
			if value == "" {
				m.err = "subscription URL cannot be empty"
				return m, nil
			}
			m.busy = true
			m.flash = ""
			m.err = ""
			return m, m.importCmd(value)
		}
	}

	if m.activePage == pageConfig && m.configFocus == configFocusInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("hnet"))
	if header := m.renderHeaderStatus(); header != "" {
		b.WriteString("  ")
		b.WriteString(header)
	}
	b.WriteString("\n\n")
	b.WriteString(m.renderPageTabs())
	b.WriteString("\n\n")

	if m.busy {
		b.WriteString(hintStyle.Render("Working..."))
		b.WriteString("\n")
	}
	if m.flash != "" {
		b.WriteString(okStyle.Render(m.flash))
		b.WriteString("\n")
	}
	if m.err != "" {
		b.WriteString(errorStyle.Render(m.err))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	if m.activePage == pageNodes {
		b.WriteString(m.renderNodesPage())
		return b.String()
	}

	b.WriteString(m.renderConfigPage())
	return b.String()
}

func (m *model) syncFromStatus(selectedProxyID string, selectedSubscriptionID string) {
	if m.status == nil {
		return
	}
	if len(m.status.AvailableProxies) == 0 {
		m.cursor = 0
	} else {
		targetProxyID := selectedProxyID
		if targetProxyID == "" {
			targetProxyID = m.currentProxyID()
		}
		foundProxy := false
		for i, proxy := range m.status.AvailableProxies {
			if proxyIdentity(proxy) == targetProxyID {
				m.cursor = i
				foundProxy = true
				break
			}
		}
		if !foundProxy {
			for i, proxy := range m.status.AvailableProxies {
				if proxy.Name == m.status.CurrentProxy {
					m.cursor = i
					foundProxy = true
					break
				}
			}
		}
		if !foundProxy {
			m.cursor = 0
		}
		if m.cursor >= len(m.status.AvailableProxies) {
			m.cursor = len(m.status.AvailableProxies) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
	}

	if len(m.status.Subscriptions) == 0 {
		m.subscriptionCursor = 0
		return
	}
	targetSubscriptionID := selectedSubscriptionID
	if targetSubscriptionID == "" {
		targetSubscriptionID = m.currentSubscriptionID()
	}
	foundSubscription := false
	for i, subscription := range m.status.Subscriptions {
		if subscriptionIdentity(subscription) == targetSubscriptionID {
			m.subscriptionCursor = i
			foundSubscription = true
			break
		}
	}
	currentSubscriptionID := m.currentSubscriptionID()
	if !foundSubscription && targetSubscriptionID != currentSubscriptionID {
		for i, subscription := range m.status.Subscriptions {
			if subscriptionIdentity(subscription) == currentSubscriptionID {
				m.subscriptionCursor = i
				foundSubscription = true
				break
			}
		}
	}
	if !foundSubscription {
		m.subscriptionCursor = 0
	}
	if m.subscriptionCursor >= len(m.status.Subscriptions) {
		m.subscriptionCursor = len(m.status.Subscriptions) - 1
	}
	if m.subscriptionCursor < 0 {
		m.subscriptionCursor = 0
	}
}

func (m *model) togglePage() {
	if m.activePage == pageNodes {
		m.activePage = pageConfig
		m.configFocus = configFocusSubscriptions
		m.resetSubscriptionInput()
		return
	}
	m.activePage = pageNodes
	m.resetSubscriptionInput()
}

func (m *model) moveCursor(delta int) {
	proxies := m.availableProxies()
	if len(proxies) == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(proxies) {
		m.cursor = len(proxies) - 1
	}
}

func (m *model) moveSubscriptionCursor(delta int) {
	subscriptions := m.availableSubscriptions()
	if len(subscriptions) == 0 {
		return
	}
	m.subscriptionCursor += delta
	if m.subscriptionCursor < 0 {
		m.subscriptionCursor = 0
	}
	if m.subscriptionCursor >= len(subscriptions) {
		m.subscriptionCursor = len(subscriptions) - 1
	}
}

func (m model) renderPageTabs() string {
	return strings.Join([]string{
		focusTitle("Config", m.activePage == pageConfig),
		focusTitle("Nodes", m.activePage == pageNodes),
	}, "   ")
}

func (m model) renderNodesPage() string {
	if m.status == nil {
		return hintStyle.Render(fmt.Sprintf("Waiting for hnetd. Start it with `hnetd start` or `hnetd serve`. Socket: %s", m.paths.SocketPath))
	}

	lines := []string{
		hintStyle.Render("Enter switch node  t test speed/latency  p toggle system proxy  Tab subscriptions  r refresh  q quit"),
		fmt.Sprintf("current: %s", emptyFallback(m.status.CurrentProxy, "not selected")),
		"",
		m.renderProxies(),
	}
	if m.status.LastError != "" {
		lines = append(lines, "", errorStyle.Render("last error: "+m.status.LastError))
	}
	return strings.Join(lines, "\n")
}

func (m model) renderConfigPage() string {
	lines := []string{
		hintStyle.Render(m.configHelpText()),
		"",
		m.renderConfigSummary(),
	}
	if op := m.renderSubscriptionOperation(); op != "" {
		lines = append(lines, "", op)
	}
	lines = append(lines,
		"",
		focusTitle("Subscriptions", m.activePage == pageConfig && m.configFocus == configFocusSubscriptions),
		m.renderSubscriptions(),
	)
	if confirm := m.renderDeleteConfirmation(); confirm != "" {
		lines = append(lines, "", confirm)
	}
	if m.configFocus == configFocusInput {
		lines = append(lines,
			"",
			focusTitle(m.subscriptionInputTitle(), true),
			m.input.View(),
			hintStyle.Render("Ctrl+S import  Esc cancel"),
		)
	}
	return strings.Join(lines, "\n")
}

func (m model) renderHeaderStatus() string {
	if m.status == nil {
		return hintStyle.Render("daemon offline")
	}
	parts := []string{
		statusChip("mihomo", m.status.Running),
		statusChip("proxy", m.status.SystemProxyEnabled),
		fmt.Sprintf("sub %s", activeSubscriptionName(m.status)),
	}
	if m.status.Running && m.status.MixedPort > 0 {
		parts = append(parts, fmt.Sprintf("mixed %d", m.status.MixedPort))
	}
	return strings.Join(parts, "  ")
}

func (m model) renderConfigSummary() string {
	if m.status == nil {
		return hintStyle.Render(fmt.Sprintf("Waiting for hnetd. Start it with `hnetd start` or `hnetd serve`. Socket: %s", m.paths.SocketPath))
	}

	lines := []string{
		fmt.Sprintf("active: %s", activeSubscriptionName(m.status)),
		fmt.Sprintf("saved: %d  current proxy: %s", len(m.status.Subscriptions), emptyFallback(m.status.CurrentProxy, "not selected")),
	}
	if m.status.LastSyncAt != nil {
		lines = append(lines, fmt.Sprintf("last sync: %s", m.status.LastSyncAt.Local().Format("2006-01-02 15:04:05")))
	}
	if m.status.LastError != "" {
		lines = append(lines, errorStyle.Render("last error: "+m.status.LastError))
	}
	if m.status.Hint != "" {
		lines = append(lines, hintStyle.Render(m.status.Hint))
	}
	return strings.Join(lines, "\n")
}

func (m model) renderStatusSummary() string {
	if m.status == nil {
		return hintStyle.Render(fmt.Sprintf("Waiting for hnetd. Start it with `hnetd start` or `hnetd serve`. Socket: %s", m.paths.SocketPath))
	}

	running := "stopped"
	if m.status.Running {
		running = "running"
	}
	systemProxy := "off"
	if m.status.SystemProxyEnabled {
		systemProxy = "on"
	}

	lines := []string{
		fmt.Sprintf("daemon: %s  mihomo: %s  system proxy: %s", m.status.DaemonVersion, running, systemProxy),
		fmt.Sprintf("active subscription: %s  saved: %d", activeSubscriptionName(m.status), len(m.status.Subscriptions)),
		fmt.Sprintf("current proxy: %s", emptyFallback(m.status.CurrentProxy, "not selected")),
		fmt.Sprintf("mixed port: %d  controller port: %d", m.status.MixedPort, m.status.ControllerPort),
	}
	if m.status.LastSyncAt != nil {
		lines = append(lines, fmt.Sprintf("last sync: %s", m.status.LastSyncAt.Local().Format("2006-01-02 15:04:05")))
	}
	if m.status.LastError != "" {
		lines = append(lines, errorStyle.Render("last error: "+m.status.LastError))
	}
	if m.status.Hint != "" {
		lines = append(lines, hintStyle.Render(m.status.Hint))
	}

	return strings.Join(lines, "\n")
}

func (m model) renderStatusPaths() string {
	if m.status == nil {
		return hintStyle.Render(fmt.Sprintf("Socket: %s", m.paths.SocketPath))
	}

	lines := []string{
		fmt.Sprintf("config: %s", m.status.ConfigPath),
		fmt.Sprintf("mihomo log: %s", m.status.LogPath),
	}
	if m.status.MihomoPath != "" {
		lines = append(lines, fmt.Sprintf("mihomo binary: %s", m.status.MihomoPath))
	}
	return strings.Join(lines, "\n")
}

func (m model) renderProxies() string {
	proxies := m.availableProxies()
	if m.status == nil || len(proxies) == 0 {
		return hintStyle.Render("No nodes yet. Open Config and import a subscription.")
	}

	start, end := proxyWindow(len(proxies), m.cursor, 12)
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		proxy := proxies[i]
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}
		current := " "
		if proxy.Name == m.status.CurrentProxy {
			current = "*"
		}
		alive := "down"
		style := disabledStyle
		if proxy.Alive {
			alive = "up"
			style = currentStyle
		}

		line := fmt.Sprintf(
			"%s%s %s [%s]  speed:%s  latency:%s",
			cursor,
			current,
			proxy.Name,
			alive,
			formatSpeed(proxy.SpeedBPS),
			formatLatency(proxy.LatencyMS),
		)
		if proxy.Type != "" {
			line += fmt.Sprintf(" %s", hintStyle.Render(proxy.Type))
		}
		lines = append(lines, style.Render(line))
	}
	if end < len(proxies) {
		lines = append(lines, hintStyle.Render(fmt.Sprintf("... %d more", len(proxies)-end)))
	}
	return strings.Join(lines, "\n")
}

func (m model) availableProxies() []api.ProxyOption {
	if m.status == nil {
		return nil
	}
	return m.status.AvailableProxies
}

func (m model) availableSubscriptions() []api.SubscriptionOption {
	if m.status == nil {
		return nil
	}
	return m.status.Subscriptions
}

func (m model) subscriptionOpRunning() bool {
	return m.status != nil && m.status.SubscriptionOp != nil && m.status.SubscriptionOp.State == "running"
}

func (m model) selectedProxy() (api.ProxyOption, bool) {
	proxies := m.availableProxies()
	if len(proxies) == 0 || m.cursor < 0 || m.cursor >= len(proxies) {
		return api.ProxyOption{}, false
	}
	return proxies[m.cursor], true
}

func (m model) selectedProxyID() string {
	proxy, ok := m.selectedProxy()
	if !ok {
		return ""
	}
	return proxyIdentity(proxy)
}

func (m model) selectedSubscription() (api.SubscriptionOption, bool) {
	subscriptions := m.availableSubscriptions()
	if len(subscriptions) == 0 || m.subscriptionCursor < 0 || m.subscriptionCursor >= len(subscriptions) {
		return api.SubscriptionOption{}, false
	}
	return subscriptions[m.subscriptionCursor], true
}

func (m model) selectedSubscriptionID() string {
	subscription, ok := m.selectedSubscription()
	if !ok {
		return ""
	}
	return subscriptionIdentity(subscription)
}

func (m model) currentProxyID() string {
	for _, proxy := range m.availableProxies() {
		if proxy.Name == m.status.CurrentProxy {
			return proxyIdentity(proxy)
		}
	}
	return ""
}

func (m model) currentSubscriptionID() string {
	for _, subscription := range m.availableSubscriptions() {
		if subscription.URL == m.status.SubscriptionURL {
			return subscriptionIdentity(subscription)
		}
	}
	return ""
}

func proxyIdentity(proxy api.ProxyOption) string {
	if strings.TrimSpace(proxy.ID) != "" {
		return proxy.ID
	}
	return strings.TrimSpace(proxy.Name)
}

func subscriptionIdentity(subscription api.SubscriptionOption) string {
	if strings.TrimSpace(subscription.ID) != "" {
		return subscription.ID
	}
	return strings.TrimSpace(subscription.URL)
}

func (m model) fetchStatusCmd() tea.Cmd {
	return func() tea.Msg {
		status, err := m.client.Status()
		return statusMsg{status: status, err: err}
	}
}

func pollStatusCmd() tea.Cmd {
	return tea.Tick(350*time.Millisecond, func(time.Time) tea.Msg {
		return pollStatusMsg{}
	})
}

func (m model) importCmd(url string) tea.Cmd {
	return func() tea.Msg {
		status, err := m.client.ImportSubscription(url)
		return actionMsg{status: status, err: err, flash: "subscription import started", hideURL: true}
	}
}

func (m model) selectSubscriptionCmd(url string) tea.Cmd {
	return func() tea.Msg {
		status, err := m.client.SelectSubscription(url)
		return actionMsg{status: status, err: err, flash: "subscription switch started", hideURL: true}
	}
}

func (m model) refreshSubscriptionCmd(url string) tea.Cmd {
	return func() tea.Msg {
		status, err := m.client.RefreshSubscription(url)
		return actionMsg{status: status, err: err, flash: "subscription refresh started"}
	}
}

func (m model) deleteSubscriptionCmd(url string) tea.Cmd {
	return func() tea.Msg {
		status, err := m.client.DeleteSubscription(url)
		return actionMsg{status: status, err: err, flash: "subscription deleted", hideURL: true}
	}
}

func (m model) selectProxyCmd(name string) tea.Cmd {
	return func() tea.Msg {
		status, err := m.client.SelectProxy(name)
		return actionMsg{status: status, err: err, flash: "proxy switched to " + name}
	}
}

func (m model) testProxyCmd(name string) tea.Cmd {
	return func() tea.Msg {
		status, err := m.client.TestProxy(name)
		return actionMsg{status: status, err: err, flash: "tested " + name}
	}
}

func (m model) systemProxyCmd(enabled bool, flash string) tea.Cmd {
	return func() tea.Msg {
		status, err := m.client.SetSystemProxy(enabled)
		return actionMsg{status: status, err: err, flash: flash}
	}
}

func (m model) renderSubscriptions() string {
	subscriptions := m.availableSubscriptions()
	if len(subscriptions) == 0 {
		return hintStyle.Render("No subscriptions yet. Press a to add a new subscription URL.")
	}

	start, end := proxyWindow(len(subscriptions), m.subscriptionCursor, 8)
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		subscription := subscriptions[i]
		cursor := "  "
		if i == m.subscriptionCursor && m.configFocus == configFocusSubscriptions {
			cursor = "> "
		}
		current := " "
		if subscription.URL == m.status.SubscriptionURL {
			current = "*"
		}
		line := fmt.Sprintf("%s%s %s", cursor, current, subscriptionDisplayName(subscription))
		style := disabledStyle
		if subscription.URL == m.status.SubscriptionURL {
			style = currentStyle
		}
		lines = append(lines, style.Render(line))
	}
	if end < len(subscriptions) {
		lines = append(lines, hintStyle.Render(fmt.Sprintf("... %d more", len(subscriptions)-end)))
	}
	return strings.Join(lines, "\n")
}

func (m model) renderSubscriptionOperation() string {
	if m.status == nil || m.status.SubscriptionOp == nil {
		return ""
	}
	op := m.status.SubscriptionOp
	label := subscriptionDisplayName(api.SubscriptionOption{URL: op.TargetURL})
	switch op.State {
	case "running":
		return hintStyle.Render(fmt.Sprintf("%s in progress: %s", op.Kind, label))
	case "failed":
		return errorStyle.Render(fmt.Sprintf("%s failed: %s", op.Kind, emptyFallback(op.Message, label)))
	case "succeeded":
		return okStyle.Render(fmt.Sprintf("%s completed: %s", op.Kind, label))
	default:
		return ""
	}
}

func (m model) renderDeleteConfirmation() string {
	if strings.TrimSpace(m.deleteConfirmURL) == "" {
		return ""
	}
	label := subscriptionDisplayName(api.SubscriptionOption{URL: m.deleteConfirmURL})
	return errorStyle.Render(fmt.Sprintf("Delete %s? Enter confirm  Esc cancel", label))
}

func (m *model) focusSubscriptionInput(value string) {
	m.configFocus = configFocusInput
	m.deleteConfirmURL = ""
	m.input.Focus()
	m.input.SetValue(value)
}

func (m *model) resetSubscriptionInput() {
	m.deleteConfirmURL = ""
	m.input.SetValue("")
	m.input.Blur()
}

func (m *model) finishSubscriptionInput() {
	m.configFocus = configFocusSubscriptions
	m.resetSubscriptionInput()
}

func (m model) configHelpText() string {
	if m.deleteConfirmURL != "" {
		return "Enter confirm delete  Esc cancel  Tab nodes  r refresh  q quit"
	}
	if m.subscriptionOpRunning() {
		return "Subscription operation running  Tab nodes  r refresh  q quit"
	}
	if m.configFocus == configFocusInput {
		return "Ctrl+S import  Esc cancel  p toggle system proxy  Tab nodes  r refresh  q quit"
	}
	if len(m.availableSubscriptions()) == 0 {
		return "a add  p toggle system proxy  Tab nodes  r refresh  q quit"
	}
	return "a add  Enter switch  u refresh active  d delete  p toggle system proxy  Tab nodes  r refresh  q quit"
}

func (m model) subscriptionInputTitle() string {
	if strings.TrimSpace(m.input.Value()) == "" {
		return "Add Subscription"
	}
	return "Add Subscription"
}

func emptyFallback(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func focusTitle(title string, focused bool) string {
	if focused {
		return focusStyle.Render(title)
	}
	return labelStyle.Render(title)
}

func statusChip(label string, enabled bool) string {
	if enabled {
		return okStyle.Render(label + " on")
	}
	return disabledStyle.Render(label + " off")
}

func formatLatency(latencyMS int) string {
	if latencyMS <= 0 {
		return "--"
	}
	return fmt.Sprintf("%dms", latencyMS)
}

func formatSpeed(speedBPS int64) string {
	if speedBPS <= 0 {
		return "--"
	}
	if speedBPS >= 1024*1024 {
		return fmt.Sprintf("%.1fMB/s", float64(speedBPS)/(1024*1024))
	}
	if speedBPS >= 1024 {
		return fmt.Sprintf("%.0fKB/s", float64(speedBPS)/1024)
	}
	return fmt.Sprintf("%dB/s", speedBPS)
}

func subscriptionDisplayName(subscription api.SubscriptionOption) string {
	if strings.TrimSpace(subscription.Name) != "" {
		return truncateMiddle(subscription.Name, 32)
	}
	return "subscription"
}

func activeSubscriptionName(status *api.StatusResponse) string {
	if status == nil || strings.TrimSpace(status.SubscriptionURL) == "" {
		return "not imported"
	}
	for _, subscription := range status.Subscriptions {
		if subscription.URL == status.SubscriptionURL {
			return subscriptionDisplayName(subscription)
		}
	}
	return "saved subscription"
}

func truncateMiddle(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	left := (max - 3) / 2
	right := max - 3 - left
	return value[:left] + "..." + value[len(value)-right:]
}

func proxyWindow(total int, cursor int, size int) (int, int) {
	if total <= size {
		return 0, total
	}
	start := cursor - size/2
	if start < 0 {
		start = 0
	}
	end := start + size
	if end > total {
		end = total
		start = end - size
	}
	return start, end
}
