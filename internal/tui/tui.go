package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spance/intun/internal/config"
	"github.com/spance/intun/internal/platform"
	"github.com/spance/intun/internal/tunnel"
	"golang.org/x/term"
)

type AuthRequest = platform.AuthRequest
type AuthResponse = platform.AuthResponse

type Screen int

const (
	ScreenMain Screen = iota
	ScreenSelectHost
	ScreenSelectType
	ScreenInputPort
)

var (
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7C3AED")).
			Bold(true).
			Padding(0, 1)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#10B981"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EF4444"))

	borderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#6B7280")).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9CA3AF")).
			Bold(true)

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Bold(true)

	runningBadgeStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(lipgloss.Color("#10B981")).
				Bold(true).
				Padding(0, 1)

	stoppedBadgeStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(lipgloss.Color("#6B7280")).
				Padding(0, 1)

	errorBadgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#EF4444")).
			Bold(true).
			Padding(0, 1)

	connectingBadgeStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#000000")).
				Background(lipgloss.Color("#F59E0B")).
				Padding(0, 1)

	labelHighlightStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#F59E0B")).
				Bold(true)

	labelSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FBBF24")).
				Bold(true)

	shortcutStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280"))

	keyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F59E0B")).
			Bold(true)

	lineStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280"))
)

type AuthPromptQueue struct {
	pending     []AuthRequest
	current     *AuthRequest
	notified    bool
	requestChan chan AuthRequest
	mu          sync.Mutex
}

func NewAuthPromptQueue() *AuthPromptQueue {
	return &AuthPromptQueue{
		pending:     make([]AuthRequest, 0),
		requestChan: make(chan AuthRequest, 10),
		notified:    false,
	}
}

func (q *AuthPromptQueue) RequestChan() chan<- AuthRequest {
	return q.requestChan
}

func (q *AuthPromptQueue) Poll() AuthRequest {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.current != nil && q.notified {
		return AuthRequest{}
	}

	select {
	case req := <-q.requestChan:
		if q.current == nil {
			q.current = &req
			q.notified = false
			return req
		}
		q.pending = append(q.pending, req)
		return AuthRequest{}
	default:
		if q.current != nil && !q.notified {
			q.notified = true
			return *q.current
		}
		return AuthRequest{}
	}
}

func (q *AuthPromptQueue) Current() *AuthRequest {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.current
}

func (q *AuthPromptQueue) Complete(resp AuthResponse) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.current != nil && q.current.Response != nil {
		q.current.Response <- resp
	}

	if len(q.pending) > 0 {
		q.current = &q.pending[0]
		q.pending = q.pending[1:]
		q.notified = false
	} else {
		q.current = nil
		q.notified = false
	}
}

func (q *AuthPromptQueue) CancelAll(id int) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.current != nil && q.current.ID == id {
		if q.current.Response != nil {
			q.current.Response <- AuthResponse{Accept: false}
		}
		q.current = nil
	}

	newPending := make([]AuthRequest, 0)
	for _, req := range q.pending {
		if req.ID == id {
			if req.Response != nil {
				req.Response <- AuthResponse{Accept: false}
			}
		} else {
			newPending = append(newPending, req)
		}
	}
	q.pending = newPending
}

type Model struct {
	screen        Screen
	manager       *tunnel.Manager
	hosts         []config.Host
	hostList      list.Model
	typeList      list.Model
	selectedHost  config.Host
	selectedType  tunnel.TunnelType
	localPort     string
	remotePort    string
	portInput     string
	inputMode     int
	selectedIndex int
	width         int
	height        int
	version       string
	err           error
	authQueue     *AuthPromptQueue
	promptMode    bool
	promptInput   string
	authCtx       *platform.AuthContext
	cancelCtx     context.Context
	cancelFunc    context.CancelFunc
}

type tickMsg struct{}

type sizeMsg struct {
	width  int
	height int
}

type authRequestMsg struct {
	request AuthRequest
}

func NewModel(hosts []config.Host, manager *tunnel.Manager, version string) Model {
	hostItems := make([]list.Item, len(hosts))
	for i, h := range hosts {
		hostItems[i] = hostItem{host: h}
	}

	hostList := list.New(hostItems, newHostDelegate(), 60, 30)
	hostList.Title = "Select Host"
	hostList.SetShowStatusBar(false)
	hostList.SetFilteringEnabled(true)
	hostList.SetShowHelp(false)

	typeItems := []list.Item{
		typeItem{name: "Local (-L)", desc: "Forward local port to remote server", t: tunnel.Local},
		typeItem{name: "Remote (-R)", desc: "Forward remote port to local server", t: tunnel.Remote},
		typeItem{name: "Dynamic (-D)", desc: "SOCKS proxy on local port", t: tunnel.Dynamic},
	}

	typeList := list.New(typeItems, list.NewDefaultDelegate(), 60, 12)
	typeList.Title = "Select Tunnel Type"
	typeList.SetShowStatusBar(false)
	typeList.SetShowHelp(false)
	typeList.SetShowPagination(false)

	authQueue := NewAuthPromptQueue()
	cancelCtx, cancelFunc := context.WithCancel(context.Background())
	authCtx := &platform.AuthContext{
		RequestChan: authQueue.RequestChan(),
		Cancel:      cancelCtx,
		Timeout:     30 * time.Second,
	}
	manager.SetAuthContext(authCtx)

	return Model{
		screen:        ScreenMain,
		manager:       manager,
		hosts:         hosts,
		hostList:      hostList,
		typeList:      typeList,
		authQueue:     authQueue,
		authCtx:       authCtx,
		cancelCtx:     cancelCtx,
		cancelFunc:    cancelFunc,
		selectedIndex: 0,
		width:         120,
		version:       version,
	}
}

type hostItem struct {
	host config.Host
}

func (h hostItem) FilterValue() string {
	v := h.host.Name
	if len(h.host.Labels) > 0 {
		v += " " + strings.Join(h.host.Labels, " ")
	}
	return v
}

func (h hostItem) Title() string {
	host := h.host.Hostname
	if host == "" {
		host = h.host.Name
	}
	if len(h.host.Labels) > 0 {
		return host + " # " + strings.Join(h.host.Labels, ", ")
	}
	return host
}

func (h hostItem) Description() string {
	return fmt.Sprintf("%s@%s:%s", h.host.User, h.host.Hostname, h.host.Port)
}

type hostDelegate struct {
	styles list.DefaultItemStyles
}

func newHostDelegate() hostDelegate {
	return hostDelegate{styles: list.NewDefaultItemStyles()}
}

func (d hostDelegate) Height() int  { return 2 }
func (d hostDelegate) Spacing() int { return 1 }
func (d hostDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func (d hostDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	h, ok := item.(hostItem)
	if !ok {
		return
	}

	s := list.NewDefaultItemStyles()
	selected := index == m.Index()

	titleStyle := s.NormalTitle
	descStyle := s.NormalDesc
	if selected {
		titleStyle = s.SelectedTitle
		descStyle = s.SelectedDesc
	}

	host := h.host.Hostname
	if host == "" {
		host = h.host.Name
	}

	var title string
	if len(h.host.Labels) > 0 {
		labelStr := strings.Join(h.host.Labels, ", ")
		if selected {
			title = titleStyle.Render(host + " # ") + labelSelectedStyle.Render(labelStr)
		} else {
			title = titleStyle.Render(host + " # ") + labelHighlightStyle.Render(labelStr)
		}
	} else {
		title = titleStyle.Render(host)
	}

	desc := descStyle.Render(h.Description())

	fmt.Fprintf(w, "%s\n%s", title, desc)
}

type typeItem struct {
	name string
	desc string
	t    tunnel.TunnelType
}

func (t typeItem) FilterValue() string {
	return t.name
}

func (t typeItem) Title() string {
	return t.name
}

func (t typeItem) Description() string {
	return t.desc
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
			return tickMsg{}
		}),
		m.pollAuthRequests(),
	)
}

func (m Model) pollAuthRequests() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		req := m.authQueue.Poll()
		if req.Response != nil {
			return authRequestMsg{request: req}
		}
		return nil
	})
}

func checkTerminalSize() tea.Msg {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return nil
	}
	return sizeMsg{width: w, height: h}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		listHeight := msg.Height - 8
		if listHeight > 30 {
			listHeight = 30
		}
		if listHeight < 5 {
			listHeight = 5
		}
		m.hostList.SetSize(msg.Width-4, listHeight)
		m.typeList.SetSize(msg.Width-4, min(12, msg.Height-8))
		return m, nil
	case sizeMsg:
		if msg.width != m.width || msg.height != m.height {
			m.width = msg.width
			m.height = msg.height
			listHeight := msg.height - 8
			if listHeight > 30 {
				listHeight = 30
			}
			if listHeight < 5 {
				listHeight = 5
			}
			m.hostList.SetSize(msg.width-4, listHeight)
			m.typeList.SetSize(msg.width-4, min(12, msg.height-8))
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKeyPress(msg)
	case tickMsg:
		return m, tea.Batch(
			tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
				return tickMsg{}
			}),
			checkTerminalSize,
			m.pollAuthRequests(),
		)
	case authRequestMsg:
		if msg.request.Response != nil {
			m.promptMode = true
			m.promptInput = ""
		}
		return m, nil
	}

	var cmd tea.Cmd
	switch m.screen {
	case ScreenSelectHost:
		m.hostList, cmd = m.hostList.Update(msg)
	case ScreenSelectType:
		m.typeList, cmd = m.typeList.Update(msg)
	}
	return m, cmd
}

func (m Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.promptMode {
		return m.handlePromptKeys(msg)
	}

	switch m.screen {
	case ScreenMain:
		return m.handleMainKeys(msg)
	case ScreenSelectHost:
		return m.handleHostSelectKeys(msg)
	case ScreenSelectType:
		return m.handleTypeSelectKeys(msg)
	case ScreenInputPort:
		return m.handlePortInputKeys(msg)
	}
	return m, nil
}

func (m Model) handlePromptKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	current := m.authQueue.Current()
	if current == nil {
		m.promptMode = false
		return m, nil
	}

	switch msg.String() {
	case "enter":
		if current.Type == platform.AuthRequestHostKey {
			m.authQueue.Complete(AuthResponse{Accept: true})
		} else {
			m.authQueue.Complete(AuthResponse{Accept: true, Password: m.promptInput})
		}
		m.promptMode = false
		return m, m.pollAuthRequests()
	case "a":
		if current.Type == platform.AuthRequestHostKey {
			m.authQueue.Complete(AuthResponse{Accept: true})
			m.promptMode = false
			return m, m.pollAuthRequests()
		}
		return m, nil
	case "r", "esc":
		m.authQueue.Complete(AuthResponse{Accept: false})
		m.promptMode = false
		return m, m.pollAuthRequests()
	case "backspace":
		if len(m.promptInput) > 0 {
			m.promptInput = m.promptInput[:len(m.promptInput)-1]
		}
		return m, nil
	default:
		if current.Type == platform.AuthRequestPassword {
			if msg.Type == tea.KeySpace {
				m.promptInput += " "
			} else if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
				m.promptInput += string(msg.Runes)
			}
		}
		return m, nil
	}
}

func (m Model) handleMainKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	tunnels := m.manager.List()

	switch msg.String() {
	case "c":
		if len(m.hosts) == 0 {
			m.err = fmt.Errorf("no hosts found in ~/.ssh/config")
			return m, nil
		}
		m.screen = ScreenSelectHost
		return m, nil
	case "y":
		if len(tunnels) > 0 && m.selectedIndex < len(tunnels) {
			t := tunnels[m.selectedIndex]
			if t.Status == tunnel.StatusError && strings.Contains(t.Error, "HOST_KEY_NOT_CACHED") {
				m.manager.Restart(t.ID)
			}
		}
	case "r":
		if len(tunnels) > 0 && m.selectedIndex < len(tunnels) {
			m.manager.Restart(tunnels[m.selectedIndex].ID)
		}
	case "s":
		if len(tunnels) > 0 && m.selectedIndex < len(tunnels) {
			t := tunnels[m.selectedIndex]
			if t.Status == tunnel.StatusRunning {
				m.manager.Stop(t.ID)
			} else if t.Status == tunnel.StatusStopped {
				m.manager.Restart(t.ID)
			}
		}
	case "d":
		if len(tunnels) > 0 && m.selectedIndex < len(tunnels) {
			m.manager.Delete(tunnels[m.selectedIndex].ID)
			if m.selectedIndex > 0 {
				m.selectedIndex--
			}
		}
	case "up", "k":
		if m.selectedIndex > 0 {
			m.selectedIndex--
		}
	case "down", "j":
		if m.selectedIndex < len(tunnels)-1 {
			m.selectedIndex++
		}
	case "e", "q", "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) handleHostSelectKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if item, ok := m.hostList.SelectedItem().(hostItem); ok {
			m.selectedHost = item.host
			m.screen = ScreenSelectType
			return m, nil
		}
	case "esc", "q":
		m.screen = ScreenMain
		return m, nil
	}
	var cmd tea.Cmd
	m.hostList, cmd = m.hostList.Update(msg)
	return m, cmd
}

func (m Model) handleTypeSelectKeys(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		m.typeList, cmd = m.typeList.Update(msg)
		return m, cmd
	}

	switch keyMsg.String() {
	case "enter":
		if item, ok := m.typeList.SelectedItem().(typeItem); ok {
			m.selectedType = item.t
			m.screen = ScreenInputPort
			m.portInput = ""
			m.inputMode = 0
			return m, nil
		}
	case "esc", "q":
		m.screen = ScreenSelectHost
		return m, nil
	}
	var cmd tea.Cmd
	m.typeList, cmd = m.typeList.Update(msg)
	return m, cmd
}

func (m Model) handlePortInputKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if m.selectedType == tunnel.Dynamic {
			m.localPort = m.portInput
			cfg := &platform.SSHConfig{
				Host:         m.selectedHost.Hostname,
				Port:         m.selectedHost.Port,
				User:         m.selectedHost.User,
				IdentityFile: m.selectedHost.IdentityFile,
			}
			m.manager.Create(m.selectedHost.Name, cfg, m.selectedType, m.localPort, "")
			m.screen = ScreenMain
			return m, nil
		}
		if m.selectedType == tunnel.Remote {
			if m.inputMode == 0 {
				if strings.Contains(m.portInput, ":") {
					m.localPort = m.portInput
				} else {
					m.localPort = "127.0.0.1:" + m.portInput
				}
				m.portInput = ""
				m.inputMode = 1
				return m, nil
			}
			m.remotePort = m.portInput
			cfg := &platform.SSHConfig{
				Host:         m.selectedHost.Hostname,
				Port:         m.selectedHost.Port,
				User:         m.selectedHost.User,
				IdentityFile: m.selectedHost.IdentityFile,
			}
			m.manager.Create(m.selectedHost.Name, cfg, m.selectedType, m.localPort, m.remotePort)
			m.screen = ScreenMain
			return m, nil
		}
		if m.inputMode == 0 {
			m.localPort = m.portInput
			m.portInput = ""
			m.inputMode = 1
			return m, nil
		}
		m.remotePort = m.portInput
		cfg := &platform.SSHConfig{
			Host:         m.selectedHost.Hostname,
			Port:         m.selectedHost.Port,
			User:         m.selectedHost.User,
			IdentityFile: m.selectedHost.IdentityFile,
		}
		m.manager.Create(m.selectedHost.Name, cfg, m.selectedType, m.localPort, m.remotePort)
		m.screen = ScreenMain
		return m, nil
	case "esc", "q":
		m.screen = ScreenSelectType
		return m, nil
	case "backspace":
		if len(m.portInput) > 0 {
			m.portInput = m.portInput[:len(m.portInput)-1]
		}
	default:
		ch := msg.String()
		if len(ch) == 1 {
			c := ch[0]
			if m.selectedType == tunnel.Remote && m.inputMode == 0 {
				if (c >= '0' && c <= '9') || c == '.' || c == ':' {
					m.portInput += ch
				}
			} else {
				if c >= '0' && c <= '9' {
					m.portInput += ch
				}
			}
		}
	}
	return m, nil
}

func (m Model) View() string {
	var b strings.Builder

	width := m.width
	if width < 80 {
		width = 80
	}

	title := fmt.Sprintf(" inTun - Interactive SSH Tunnel (%s)", m.version)
	titlePadding := width - len(title)
	if titlePadding > 0 {
		title = title + strings.Repeat(" ", titlePadding)
	}
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")

	if m.promptMode {
		b.WriteString(m.renderPrompt())
		b.WriteString("\n\n")
	}

	switch m.screen {
	case ScreenMain:
		b.WriteString(m.renderMainScreen())
	case ScreenSelectHost:
		b.WriteString(m.hostList.View())
	case ScreenSelectType:
		b.WriteString(m.typeList.View())
	case ScreenInputPort:
		b.WriteString(m.renderPortInput())
	}

	content := b.String()
	lines := strings.Count(content, "\n")
	remainingLines := m.height - lines - 1
	if remainingLines > 0 {
		content += strings.Repeat("\n", remainingLines)
	}

	return content + m.renderShortcuts()
}

func (m Model) renderMainScreen() string {
	var b strings.Builder
	tunnels := m.manager.List()

	if len(tunnels) == 0 {
		b.WriteString(headerStyle.Render("No tunnels active. Press 'c' to create one."))
		b.WriteString("\n")
		return b.String()
	}

	lineWidth := m.width
	if lineWidth < 80 {
		lineWidth = 80
	}

	separator := strings.Repeat("=", lineWidth)

	fixedWidth := 2 + 4 + 1 + 3 + 13 + 1 + 6 + 1 + 8 + 1 + 8 + 1 + 8
	nameWidth := lineWidth - fixedWidth
	if nameWidth < 10 {
		nameWidth = 10
	}

	header := fmt.Sprintf("  %-4s %-*s   %-13s %-6s %8s %8s %8s",
		"#", nameWidth, "Name", " Status", "Type", "Local", "Remote", "Latency")
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")
	b.WriteString(lineStyle.Render(separator))
	b.WriteString("\n")

	for i, t := range tunnels {
		var status string
		var badgeStyle lipgloss.Style
		switch t.Status {
		case tunnel.StatusRunning:
			status = "Running"
			badgeStyle = runningBadgeStyle
		case tunnel.StatusError:
			status = "Error"
			badgeStyle = errorBadgeStyle
		case tunnel.StatusConnecting:
			status = "Connecting"
			badgeStyle = connectingBadgeStyle
		case tunnel.StatusStopped:
			status = "Stopped"
			badgeStyle = stoppedBadgeStyle
		}

		remote := "-"
		if t.Type == tunnel.Dynamic {
			remote = "SOCKS5"
		} else if t.RemotePort != "" {
			remote = ":" + t.RemotePort
		}

		latency := "-"
		if t.Latency > 0 {
			latency = fmt.Sprintf("%dms", t.Latency.Milliseconds())
		}

		prefix := "  "
		if i == m.selectedIndex {
			prefix = "→ "
		}

		badge := badgeStyle.Render(status)

		if i == m.selectedIndex {
			line := fmt.Sprintf("%s%-4d %-*s   %s %-6s %8s %8s %8s",
				prefix, t.ID, nameWidth, truncate(t.Name, nameWidth), badge,
				t.Type.String(), ":"+t.LocalPort, remote, latency)
			b.WriteString(selectedStyle.Render(line))
		} else {
			line := fmt.Sprintf("%s%-4d %-*s   %s %-6s %8s %8s %8s",
				prefix, t.ID, nameWidth, truncate(t.Name, nameWidth), badge,
				t.Type.String(), ":"+t.LocalPort, remote, latency)
			b.WriteString(line)
		}
		b.WriteString("\n")

		speedLine := fmt.Sprintf("%12s    %12s    %12s    %12s",
			formatSpeed(t.UploadSpeed, "↑"), formatSpeed(t.DownloadSpeed, "↓"),
			formatTotal(t.UploadBytes, "↑"), formatTotal(t.DownloadBytes, "↓"))
		if i == m.selectedIndex {
			b.WriteString(lipgloss.NewStyle().Width(lineWidth).Foreground(lipgloss.Color("#C4B5FD")).Align(lipgloss.Right).Render(speedLine))
		} else {
			b.WriteString(lipgloss.NewStyle().Width(lineWidth).Foreground(lipgloss.Color("#6B7280")).Align(lipgloss.Right).Render(speedLine))
		}
		b.WriteString("\n")

		if t.Status == tunnel.StatusError && t.Error != "" {
			errMsg := t.Error
			if strings.Contains(errMsg, "SSH_AUTH_FAILED") {
				b.WriteString(errorStyle.Render("      Authentication failed. Check SSH key:"))
				b.WriteString("\n")
				b.WriteString(shortcutStyle.Render("      Ensure valid key in ~/.ssh/id_rsa or ~/.ssh/id_ed25519"))
				b.WriteString("\n")
				b.WriteString(shortcutStyle.Render("      Or specify IdentityFile in ~/.ssh/config"))
				b.WriteString("\n")
			} else if strings.Contains(errMsg, "SSH_CONNECTION_FAILED") {
				b.WriteString(errorStyle.Render("      Connection failed:"))
				b.WriteString("\n")
				errDetail := truncate(errMsg, lineWidth-10)
				b.WriteString(shortcutStyle.Render("      " + errDetail))
				b.WriteString("\n")
			} else if strings.Contains(errMsg, "HOST_KEY_NOT_CACHED") {
				b.WriteString(errorStyle.Render("      Host key not cached. Run manually:"))
				b.WriteString("\n")
				cmdLine := fmt.Sprintf("      ssh %s@%s -p %s", t.SSHConfig.User, t.SSHConfig.Host, t.SSHConfig.Port)
				b.WriteString(selectedStyle.Render(cmdLine))
				b.WriteString("\n")
			} else if strings.Contains(errMsg, "connection lost") {
				b.WriteString(errorStyle.Render("      SSH connection lost - press 'r' to reconnect"))
				b.WriteString("\n")
			} else {
				errLine := fmt.Sprintf("      Error: %s", truncate(errMsg, lineWidth-16))
				b.WriteString(errorStyle.Render(errLine))
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderPortInput() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("Enter Port Number"))
	b.WriteString("\n\n")

	if m.selectedType == tunnel.Dynamic {
		b.WriteString(fmt.Sprintf("SOCKS Proxy Port: %s", m.portInput))
		b.WriteString(shortcutStyle.Render("_"))
	} else if m.selectedType == tunnel.Remote {
		if m.inputMode == 0 {
			b.WriteString(fmt.Sprintf("Local Target (ip:port or port): %s", m.portInput))
			b.WriteString(shortcutStyle.Render("_"))
		} else {
			b.WriteString(fmt.Sprintf("Local Target: %s\n", m.localPort))
			b.WriteString(fmt.Sprintf("Remote Listen Port: %s", m.portInput))
			b.WriteString(shortcutStyle.Render("_"))
		}
	} else {
		if m.inputMode == 0 {
			b.WriteString(fmt.Sprintf("Local Port: %s", m.portInput))
			b.WriteString(shortcutStyle.Render("_"))
		} else {
			b.WriteString(fmt.Sprintf("Local Port: %s\n", m.localPort))
			b.WriteString(fmt.Sprintf("Remote Port: %s", m.portInput))
			b.WriteString(shortcutStyle.Render("_"))
		}
	}
	b.WriteString("\n\n")
	b.WriteString(shortcutStyle.Render("Press Enter to confirm, Esc to cancel"))
	return b.String()
}

func (m Model) renderPrompt() string {
	current := m.authQueue.Current()
	if current == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString(borderStyle.Render(" Auth Required "))
	b.WriteString("\n\n")

	if current.Type == platform.AuthRequestHostKey {
		b.WriteString(fmt.Sprintf("Unknown host key for: %s\n", current.Host))
		b.WriteString(fmt.Sprintf("Fingerprint: %s\n\n", current.Fingerprint))
		b.WriteString(shortcutStyle.Render("[A] Accept  [R] Reject"))
	} else {
		attempt := current.RetryCount + 1
		b.WriteString(fmt.Sprintf("Password for %s (attempt %d/3):\n", current.Host, attempt))
		b.WriteString(fmt.Sprintf("[%s]\n\n", strings.Repeat("*", len(m.promptInput))))
		b.WriteString(shortcutStyle.Render("[Enter] Submit  [Esc] Cancel"))
	}

	return b.String()
}

func (m Model) renderShortcuts() string {
	width := m.width
	if width < 80 {
		width = 80
	}

	var items []string
	switch m.screen {
	case ScreenMain:
		items = []string{
			"[" + keyStyle.Render("↑↓") + "]Navigate",
			"[" + keyStyle.Render("c") + "]Create",
			"[" + keyStyle.Render("r") + "]Reconnect",
			"[" + keyStyle.Render("s") + "]Stop/Start",
			"[" + keyStyle.Render("d") + "]Delete",
			"[" + keyStyle.Render("e") + "]Exit",
		}
	case ScreenSelectHost, ScreenSelectType:
		items = []string{
			"[" + keyStyle.Render("↑↓") + "]Navigate",
			"[" + keyStyle.Render("Enter") + "]Select",
			"[" + keyStyle.Render("Esc") + "]Back",
		}
	case ScreenInputPort:
		items = []string{
			"[0-9]Input Port",
			"[" + keyStyle.Render("Enter") + "]Confirm",
			"[" + keyStyle.Render("Esc") + "]Back",
		}
	}

	totalItemWidth := 0
	for _, item := range items {
		totalItemWidth += lipgloss.Width(item)
	}

	gapCount := len(items) - 1
	if gapCount <= 0 {
		gapCount = 1
	}
	gapWidth := (width - totalItemWidth) / gapCount
	if gapWidth < 1 {
		gapWidth = 1
	}

	gap := strings.Repeat(" ", gapWidth)

	var result string
	for i, item := range items {
		result += item
		if i < len(items)-1 {
			result += gap
		}
	}

	remaining := width - lipgloss.Width(result)
	if remaining > 0 {
		result += strings.Repeat(" ", remaining)
	}

	return shortcutStyle.Render(result)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func formatBytes(b int64) string {
	const KB = 1024
	const MB = KB * 1024

	if b >= MB {
		return fmt.Sprintf("%.2f MB", float64(b)/float64(MB))
	}
	return fmt.Sprintf("%.2f KB", float64(b)/float64(KB))
}

func formatSpeed(bytes int64, dir string) string {
	return fmt.Sprintf("%s/s%s", formatBytes(bytes), dir)
}

func formatTotal(bytes int64, dir string) string {
	return fmt.Sprintf("%s∑%s", formatBytes(bytes), dir)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
