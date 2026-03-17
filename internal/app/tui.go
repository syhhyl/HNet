package app

import (
	"fmt"
	"strings"

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
	actionStyle   = lipgloss.NewStyle().Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240"))
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

type model struct {
	client             *client.Client
	paths              Paths
	input              textinput.Model
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
		activePage: pageNodes,
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
		m.busy = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.err = ""
		m.status = msg.status
		m.syncFromStatus()
	case actionMsg:
		m.busy = false
		if msg.err != nil {
			m.err = msg.err.Error()
			m.flash = ""
			return m, nil
		}
		m.err = ""
		m.flash = msg.flash
		m.status = msg.status
		m.syncFromStatus()
		if msg.hideURL {
			m.finishSubscriptionInput()
		}
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
		}

		if m.activePage == pageNodes {
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

		switch msg.String() {
		case "p":
			if m.status == nil {
				m.err = "hnetd is not reachable"
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

		if m.configFocus == configFocusSubscriptions {
			switch msg.String() {
			case "a":
				m.focusSubscriptionInput("")
				return m, nil
			case "i":
				value := ""
				if subscription, ok := m.selectedSubscription(); ok {
					value = subscription.URL
				} else if m.status != nil {
					value = m.status.SubscriptionURL
				}
				m.focusSubscriptionInput(value)
				return m, nil
			case "u":
				if subscription, ok := m.selectedSubscription(); ok {
					m.busy = true
					m.flash = ""
					m.err = ""
					return m, m.updateSubscriptionCmd(subscription.URL)
				}
				return m, nil
			case "d":
				if subscription, ok := m.selectedSubscription(); ok {
					m.busy = true
					m.flash = ""
					m.err = ""
					return m, m.deleteSubscriptionCmd(subscription.URL)
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
					m.busy = true
					m.flash = ""
					m.err = ""
					if m.status != nil && subscription.URL == m.status.SubscriptionURL {
						return m, m.updateSubscriptionCmd(subscription.URL)
					}
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
	b.WriteString("\n")
	b.WriteString(m.renderPageTabs())
	b.WriteString("\n")

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

func (m *model) syncFromStatus() {
	if m.status == nil {
		return
	}
	if len(m.status.AvailableProxies) == 0 {
		m.cursor = 0
	} else {
		for i, proxy := range m.status.AvailableProxies {
			if proxy.Name == m.status.CurrentProxy {
				m.cursor = i
				break
			}
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
		if m.activePage == pageConfig {
			m.focusSubscriptionInput("")
		}
		return
	}
	for i, subscription := range m.status.Subscriptions {
		if subscription.URL == m.status.SubscriptionURL {
			m.subscriptionCursor = i
			break
		}
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
		if len(m.availableSubscriptions()) == 0 {
			m.focusSubscriptionInput("")
		} else {
			m.configFocus = configFocusSubscriptions
			m.resetSubscriptionInput()
		}
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
		focusTitle("Nodes", m.activePage == pageNodes),
		focusTitle("Config", m.activePage == pageConfig),
	}, "   ")
}

func (m model) renderNodesPage() string {
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
		hintStyle.Render("Enter switch node  t test speed/latency  Tab config  r refresh  q quit"),
		fmt.Sprintf("current: %s", emptyFallback(m.status.CurrentProxy, "not selected")),
		fmt.Sprintf("mihomo: %s  system proxy: %s  subscription: %s", running, systemProxy, activeSubscriptionName(m.status)),
		"",
		labelStyle.Render("Proxy Nodes"),
		m.renderProxies(),
	}
	if m.status.LastError != "" {
		lines = append(lines, "", errorStyle.Render("last error: "+m.status.LastError))
	}
	return strings.Join(lines, "\n")
}

func (m model) renderConfigPage() string {
	lines := []string{
		hintStyle.Render("j/k select subscription  Enter switch/update  u update  a add  i edit URL  d delete  Ctrl+S import  Esc done  p toggle system proxy  Tab nodes  r refresh  q quit"),
		labelStyle.Render("Actions"),
		m.renderConfigActions(),
		"",
		focusTitle("New Subscription URL", m.activePage == pageConfig && m.configFocus == configFocusInput),
		m.input.View(),
		"",
		focusTitle("Saved Subscriptions", m.activePage == pageConfig && m.configFocus == configFocusSubscriptions),
		m.renderSubscriptions(),
		"",
		labelStyle.Render("Config Info"),
		m.renderStatus(),
	}
	return strings.Join(lines, "\n")
}

func (m model) renderStatus() string {
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
		fmt.Sprintf("daemon: %s", m.status.DaemonVersion),
		fmt.Sprintf("mihomo: %s", running),
		fmt.Sprintf("system proxy: %s", systemProxy),
		fmt.Sprintf("current proxy: %s", emptyFallback(m.status.CurrentProxy, "not selected")),
		fmt.Sprintf("active subscription: %s", activeSubscriptionName(m.status)),
		fmt.Sprintf("subscription count: %d", len(m.status.Subscriptions)),
		fmt.Sprintf("mixed port: %d", m.status.MixedPort),
		fmt.Sprintf("controller port: %d", m.status.ControllerPort),
		fmt.Sprintf("config: %s", m.status.ConfigPath),
		fmt.Sprintf("mihomo log: %s", m.status.LogPath),
	}
	if m.status.MihomoPath != "" {
		lines = append(lines, fmt.Sprintf("mihomo binary: %s", m.status.MihomoPath))
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

func (m model) selectedProxy() (api.ProxyOption, bool) {
	proxies := m.availableProxies()
	if len(proxies) == 0 || m.cursor < 0 || m.cursor >= len(proxies) {
		return api.ProxyOption{}, false
	}
	return proxies[m.cursor], true
}

func (m model) selectedSubscription() (api.SubscriptionOption, bool) {
	subscriptions := m.availableSubscriptions()
	if len(subscriptions) == 0 || m.subscriptionCursor < 0 || m.subscriptionCursor >= len(subscriptions) {
		return api.SubscriptionOption{}, false
	}
	return subscriptions[m.subscriptionCursor], true
}

func (m model) fetchStatusCmd() tea.Cmd {
	return func() tea.Msg {
		status, err := m.client.Status()
		return statusMsg{status: status, err: err}
	}
}

func (m model) importCmd(url string) tea.Cmd {
	return func() tea.Msg {
		status, err := m.client.ImportSubscription(url)
		return actionMsg{status: status, err: err, flash: "subscription imported and mihomo restarted", hideURL: true}
	}
}

func (m model) updateSubscriptionCmd(url string) tea.Cmd {
	return func() tea.Msg {
		status, err := m.client.SelectSubscription(url)
		return actionMsg{status: status, err: err, flash: "subscription updated", hideURL: true}
	}
}

func (m model) selectSubscriptionCmd(url string) tea.Cmd {
	return func() tea.Msg {
		status, err := m.client.SelectSubscription(url)
		return actionMsg{status: status, err: err, flash: "subscription switched", hideURL: true}
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

func (m *model) focusSubscriptionInput(value string) {
	m.configFocus = configFocusInput
	m.input.Focus()
	m.input.SetValue(value)
}

func (m *model) resetSubscriptionInput() {
	m.input.SetValue("")
	m.input.Blur()
}

func (m *model) finishSubscriptionInput() {
	if len(m.availableSubscriptions()) == 0 {
		m.focusSubscriptionInput("")
		return
	}
	m.configFocus = configFocusSubscriptions
	m.resetSubscriptionInput()
}

func (m model) renderConfigActions() string {
	systemProxyLabel := "Enable System Proxy"
	if m.status != nil && m.status.SystemProxyEnabled {
		systemProxyLabel = "Disable System Proxy"
	}

	actions := []string{
		renderActionButton("a", "Add Subscription"),
		renderActionButton("u", "Update Selected"),
		renderActionButton("i", "Edit URL"),
		renderActionButton("p", systemProxyLabel),
	}
	return strings.Join(actions, "  ")
}

func emptyFallback(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func focusTitle(title string, focused bool) string {
	if focused {
		return focusStyle.Render(title + " [active]")
	}
	return labelStyle.Render(title)
}

func renderActionButton(key string, label string) string {
	return actionStyle.Render("[" + key + "] " + label)
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
