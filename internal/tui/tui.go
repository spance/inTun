package tui

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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

const (
	minTermWidth   = 80
	colIDW         = 4
	colStatusW     = 13
	colTypeW       = 10
	colAddrW       = 21
	colLatencyW    = 8
	colFixedWidth  = 2 + colIDW + 1 + 3 + colStatusW + 1 + colTypeW + 1 + colAddrW + 1 + colAddrW + 1 + colLatencyW
	defaultWidth   = 120
	defaultHeight  = 30
	hostListHeight = 30
	typeListHeight = 12
)

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

	q.drainPendingLocked()
	if q.current != nil && !q.notified {
		q.notified = true
		return *q.current
	}
	return AuthRequest{}
}

func (q *AuthPromptQueue) drainPendingLocked() {
	for {
		select {
		case req := <-q.requestChan:
			if q.current == nil {
				q.current = &req
			} else {
				q.pending = append(q.pending, req)
			}
		default:
			return
		}
	}
}

func (q *AuthPromptQueue) Current() *AuthRequest {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.current
}

func (q *AuthPromptQueue) PendingCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
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

	q.drainPendingLocked()

	if q.current != nil && q.current.ID == id {
		if q.current.Response != nil {
			q.current.Response <- AuthResponse{Accept: false}
		}
		q.current = nil
		q.notified = false
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
	confirmQuit   bool
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

	hostList := list.New(hostItems, newHostDelegate(), 60, defaultHeight)
	hostList.Title = "Select Host"
	hostList.SetShowStatusBar(false)
	hostList.SetFilteringEnabled(true)
	hostList.SetShowHelp(false)

	typeItems := []list.Item{
		typeItem{name: "Local (-L)", desc: "Forward local port to remote server", t: tunnel.Local},
		typeItem{name: "Remote (-R)", desc: "Forward remote port to local server", t: tunnel.Remote},
		typeItem{name: "Dynamic (-D)", desc: "SOCKS proxy on local port", t: tunnel.Dynamic},
	}

	typeList := list.New(typeItems, list.NewDefaultDelegate(), 60, typeListHeight)
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
		width:         defaultWidth,
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

func (d hostDelegate) Height() int                             { return 2 }
func (d hostDelegate) Spacing() int                            { return 1 }
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
			title = titleStyle.Render(host+" # ") + labelSelectedStyle.Render(labelStr)
		} else {
			title = titleStyle.Render(host+" # ") + labelHighlightStyle.Render(labelStr)
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
		listWidth, listHeight := listDimensions(msg.Width, msg.Height, hostListHeight)
		typeWidth, typeHeight := listDimensions(msg.Width, msg.Height, typeListHeight)
		m.hostList.SetSize(listWidth, listHeight)
		m.typeList.SetSize(typeWidth, typeHeight)
		return m, nil
	case sizeMsg:
		if msg.width != m.width || msg.height != m.height {
			m.width = msg.width
			m.height = msg.height
			listWidth, listHeight := listDimensions(msg.width, msg.height, hostListHeight)
			typeWidth, typeHeight := listDimensions(msg.width, msg.height, typeListHeight)
			m.hostList.SetSize(listWidth, listHeight)
			m.typeList.SetSize(typeWidth, typeHeight)
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
	if m.confirmQuit {
		return m.handleQuitConfirmKeys(msg)
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

func (m Model) handleQuitConfirmKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "y", "q", "e", "ctrl+c":
		return m, tea.Quit
	case "esc", "n":
		m.confirmQuit = false
		return m, nil
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
			m.promptInput = string([]rune(m.promptInput)[:len([]rune(m.promptInput))-1])
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
		m.err = nil
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
		if m.hasLiveTunnels() {
			m.confirmQuit = true
			return m, nil
		}
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) hasLiveTunnels() bool {
	for _, t := range m.manager.List() {
		status := t.GetStatus()
		if status == tunnel.StatusRunning || status == tunnel.StatusConnecting {
			return true
		}
	}
	return false
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

func (m Model) buildSSHConfig() *platform.SSHConfig {
	return &platform.SSHConfig{
		Host:         m.selectedHost.Hostname,
		Port:         m.selectedHost.Port,
		User:         m.selectedHost.User,
		IdentityFile: m.selectedHost.IdentityFile,
	}
}

func (m Model) handlePortInputKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if m.selectedType == tunnel.Dynamic {
			if !validPortInput(m.portInput, false) {
				m.err = fmt.Errorf("invalid SOCKS proxy port: %s", m.portInput)
				return m, nil
			}
			m.err = nil
			m.localPort = m.portInput
			m.manager.Create(m.selectedHost.Name, m.buildSSHConfig(), m.selectedType, m.localPort, "")
			m.screen = ScreenMain
			return m, nil
		}
		if m.selectedType == tunnel.Remote {
			if m.inputMode == 0 {
				if !validPortInput(m.portInput, true) {
					m.err = fmt.Errorf("invalid local target: %s", m.portInput)
					return m, nil
				}
				m.err = nil
				if strings.Contains(m.portInput, ":") {
					m.localPort = m.portInput
				} else {
					m.localPort = "127.0.0.1:" + m.portInput
				}
				m.portInput = ""
				m.inputMode = 1
				return m, nil
			}
			if !validPortInput(m.portInput, true) {
				m.err = fmt.Errorf("invalid remote listen: %s", m.portInput)
				return m, nil
			}
			m.err = nil
			if strings.Contains(m.portInput, ":") {
				m.remotePort = m.portInput
			} else {
				m.remotePort = "127.0.0.1:" + m.portInput
			}
			m.manager.Create(m.selectedHost.Name, m.buildSSHConfig(), m.selectedType, m.localPort, m.remotePort)
			m.screen = ScreenMain
			return m, nil
		}
		if m.inputMode == 0 {
			if !validPortInput(m.portInput, true) {
				m.err = fmt.Errorf("invalid local listen: %s", m.portInput)
				return m, nil
			}
			m.err = nil
			if strings.Contains(m.portInput, ":") {
				m.localPort = m.portInput
			} else {
				m.localPort = "127.0.0.1:" + m.portInput
			}
			m.portInput = ""
			m.inputMode = 1
			return m, nil
		}
		if !validPortInput(m.portInput, true) {
			m.err = fmt.Errorf("invalid remote target: %s", m.portInput)
			return m, nil
		}
		m.err = nil
		if strings.Contains(m.portInput, ":") {
			m.remotePort = m.portInput
		} else {
			m.remotePort = "127.0.0.1:" + m.portInput
		}
		m.manager.Create(m.selectedHost.Name, m.buildSSHConfig(), m.selectedType, m.localPort, m.remotePort)
		m.screen = ScreenMain
		return m, nil
	case "esc", "q":
		m.screen = ScreenSelectType
		return m, nil
	case "backspace":
		if len(m.portInput) > 0 {
			m.portInput = string([]rune(m.portInput)[:len([]rune(m.portInput))-1])
		}
	default:
		if msg.Type == tea.KeyRunes {
			allowAddr := m.selectedType == tunnel.Remote || m.selectedType == tunnel.Local
			for _, r := range msg.Runes {
				if validPortInputRune(r, allowAddr) {
					m.portInput += string(r)
				}
			}
		}
	}
	return m, nil
}

func (m Model) View() string {
	var b strings.Builder

	width := renderWidth(m.width)
	height := renderHeight(m.height)

	title := fmt.Sprintf(" inTun - Interactive SSH Tunnel (%s)", m.version)
	titleInnerWidth := width - 2
	title = truncate(title, titleInnerWidth)
	titlePadding := titleInnerWidth - lipgloss.Width(title)
	if titlePadding > 0 {
		title = title + strings.Repeat(" ", titlePadding)
	}
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n\n")

	if m.err != nil {
		b.WriteString(errorStyle.Render("Error: " + truncate(m.err.Error(), width-7)))
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
	remainingLines := height - lines - 1
	if remainingLines > 0 {
		content += strings.Repeat("\n", remainingLines)
	}

	view := content + m.renderShortcuts()
	if m.promptMode {
		return overlayCentered(view, m.renderPromptModal(width), width, height)
	}
	if m.confirmQuit {
		return overlayCentered(view, m.renderQuitConfirmModal(width), width, height)
	}
	return view
}

func (m Model) renderMainScreen() string {
	var b strings.Builder
	tunnels := m.manager.List()

	if len(tunnels) == 0 {
		b.WriteString(headerStyle.Render("No tunnels active. Press 'c' to create one."))
		b.WriteString("\n")
		return b.String()
	}

	lineWidth := renderWidth(m.width)
	layout := newTableLayout(lineWidth)
	separator := strings.Repeat("=", lineWidth)

	header := fmt.Sprintf("  %-4s %-*s   %-13s %-*s %-*s %-*s %-*s",
		"#", layout.nameW, "Name", "Status",
		layout.typeW, "Type",
		layout.addrW, "Local",
		layout.addrW, "Remote",
		layout.latencyW, "Latency")
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
			remote = formatTunnelAddr(t.RemotePort)
		}
		local := formatTunnelAddr(t.LocalPort)

		latency := "-"
		if t.Latency > 0 {
			latency = fmt.Sprintf("%dms", t.Latency.Milliseconds())
		}

		prefix := "  "
		if i == m.selectedIndex {
			prefix = "→ "
		}

		badge := badgeStyle.Render(status)
		badgeW := lipgloss.Width(badge)
		badgePad := ""
		if badgeW < colStatusW {
			badgePad = strings.Repeat(" ", colStatusW-badgeW)
		}

		if i == m.selectedIndex {
			plainPart := fmt.Sprintf("%s%-4d %-*s   ", prefix, t.ID, layout.nameW, truncate(t.Name, layout.nameW))
			afterBadge := fmt.Sprintf(" %-*s %-*s %-*s %-*s",
				layout.typeW, truncate(t.Type.String(), layout.typeW),
				layout.addrW, truncate(local, layout.addrW),
				layout.addrW, truncate(remote, layout.addrW),
				layout.latencyW, truncate(latency, layout.latencyW))
			b.WriteString(selectedStyle.Render(plainPart))
			b.WriteString(badge)
			b.WriteString(badgePad)
			b.WriteString(selectedStyle.Render(afterBadge))
		} else {
			line := fmt.Sprintf("%s%-4d %-*s   %s%s %-*s %-*s %-*s %-*s",
				prefix, t.ID, layout.nameW, truncate(t.Name, layout.nameW), badge, badgePad,
				layout.typeW, truncate(t.Type.String(), layout.typeW),
				layout.addrW, truncate(local, layout.addrW),
				layout.addrW, truncate(remote, layout.addrW),
				layout.latencyW, truncate(latency, layout.latencyW))
			b.WriteString(line)
		}
		b.WriteString("\n")

		speedLine := fmt.Sprintf("    %13s    %13s    %13s    %13s",
			formatSpeed(t.UploadSpeed, "↑"), formatSpeed(t.DownloadSpeed, "↓"),
			formatTotal(t.UploadBytes, "TX"), formatTotal(t.DownloadBytes, "RX"))
		if i == m.selectedIndex {
			b.WriteString(lipgloss.NewStyle().Width(lineWidth).Foreground(lipgloss.Color("#C4B5FD")).Render(speedLine))
		} else {
			b.WriteString(lipgloss.NewStyle().Width(lineWidth).Foreground(lipgloss.Color("#6B7280")).Render(speedLine))
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
			} else if strings.Contains(errMsg, "SSH_CONNECTION_LOST") {
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
			b.WriteString(fmt.Sprintf("Remote Listen (ip:port or port): %s", m.portInput))
			b.WriteString(shortcutStyle.Render("_"))
		}
	} else {
		if m.inputMode == 0 {
			b.WriteString(fmt.Sprintf("Local Listen (ip:port or port): %s", m.portInput))
			b.WriteString(shortcutStyle.Render("_"))
		} else {
			b.WriteString(fmt.Sprintf("Local Listen: %s\n", m.localPort))
			b.WriteString(fmt.Sprintf("Remote Target (ip:port or port): %s", m.portInput))
			b.WriteString(shortcutStyle.Render("_"))
		}
	}
	b.WriteString("\n\n")
	b.WriteString(shortcutStyle.Render("Press Enter to confirm, Esc to cancel"))
	return b.String()
}

func (m Model) renderPromptModal(width int) ModalView {
	current := m.authQueue.Current()
	if current == nil {
		return ModalView{}
	}

	modalWidth := min(64, width-8)
	if modalWidth < 40 {
		modalWidth = width - 4
	}
	contentWidth := modalWidth - 6

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().
		Background(lipgloss.Color("#EF4444")).
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true).
		Padding(0, 1).
		Render("Auth Required"))
	b.WriteString("\n")

	if current.Type == platform.AuthRequestHostKey {
		b.WriteString("\n")
		b.WriteString(headerStyle.Render("Unknown host key"))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("Host        %s\n", selectedStyle.Render(truncate(current.Host, contentWidth-12))))
		b.WriteString(fmt.Sprintf("Fingerprint %s\n", truncate(current.Fingerprint, contentWidth-12)))
		b.WriteString("\n")
		b.WriteString(shortcutStyle.Render("[A] Accept    [R] Reject"))
	} else {
		attempt := current.RetryCount + 1
		b.WriteString("\n")
		b.WriteString(headerStyle.Render(fmt.Sprintf("Password required  attempt %d/3", attempt)))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("Host %s\n", selectedStyle.Render(truncate(current.Host, contentWidth-6))))
		mask := strings.Repeat("*", len([]rune(m.promptInput)))
		b.WriteString(fmt.Sprintf("[%s]\n\n", truncate(mask, contentWidth-2)))
		b.WriteString(shortcutStyle.Render("[Enter] Submit    [Esc] Cancel"))
	}
	if pending := m.authQueue.PendingCount(); pending > 0 {
		b.WriteString("\n")
		b.WriteString(shortcutStyle.Render(fmt.Sprintf("%d more auth request(s) queued", pending)))
	}

	return renderModal(width, b.String(), modalWidth)
}

func (m Model) renderQuitConfirmModal(width int) ModalView {
	liveCount := 0
	for _, t := range m.manager.List() {
		status := t.GetStatus()
		if status == tunnel.StatusRunning || status == tunnel.StatusConnecting {
			liveCount++
		}
	}

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().
		Background(lipgloss.Color("#F59E0B")).
		Foreground(lipgloss.Color("#000000")).
		Bold(true).
		Padding(0, 1).
		Render("Confirm Exit"))
	b.WriteString("\n\n")
	b.WriteString(headerStyle.Render("Active tunnels are still running"))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("%d live tunnel(s) will be closed when inTun exits.\n", liveCount))
	b.WriteString("\n")
	b.WriteString(shortcutStyle.Render("[Enter/Y/Q] Exit    [Esc/N] Cancel"))

	return renderModal(width, b.String(), 56)
}

func (m Model) renderShortcuts() string {
	width := renderWidth(m.width)

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
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

type tableLayout struct {
	nameW    int
	typeW    int
	addrW    int
	latencyW int
}

func newTableLayout(width int) tableLayout {
	width = renderWidth(width)
	layout := tableLayout{
		nameW:    10,
		typeW:    8,
		addrW:    colAddrW,
		latencyW: 7,
	}

	fixedWithoutNameAndAddr := 2 + colIDW + 1 + 3 + colStatusW + 1 + layout.typeW + 1 + 1 + 1 + layout.latencyW
	remaining := width - fixedWithoutNameAndAddr
	layout.addrW = min(colAddrW, (remaining-layout.nameW)/2)
	if layout.addrW < 10 {
		layout.addrW = 10
	}
	layout.nameW = remaining - 2*layout.addrW
	if layout.nameW < 10 {
		deficit := 10 - layout.nameW
		layout.addrW -= (deficit + 1) / 2
		if layout.addrW < 8 {
			layout.addrW = 8
		}
		layout.nameW = remaining - 2*layout.addrW
	}
	return layout
}

func renderWidth(width int) int {
	if width < minTermWidth {
		return minTermWidth
	}
	return width
}

func renderHeight(height int) int {
	if height <= 0 {
		return defaultHeight
	}
	return height
}

type ModalView struct {
	content string
	width   int
}

func (m ModalView) String() string {
	return m.content
}

func overlayCentered(base string, modal ModalView, width, height int) string {
	if modal.content == "" {
		return base
	}

	lines := strings.Split(base, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}

	modalLines := strings.Split(modal.content, "\n")
	top := (height - len(modalLines)) / 2
	if top < 0 {
		top = 0
	}
	left := (width - modal.width) / 2
	if left < 0 {
		left = 0
	}
	prefix := strings.Repeat(" ", left)

	for i, modalLine := range modalLines {
		row := top + i
		if row >= len(lines) {
			break
		}
		lines[row] = prefix + modalLine
	}

	return strings.Join(lines, "\n")
}

func renderModal(screenWidth int, body string, modalWidth int) ModalView {
	if modalWidth <= 0 {
		modalWidth = min(64, screenWidth-8)
	}
	maxWidth := screenWidth - 4
	if modalWidth > maxWidth {
		modalWidth = maxWidth
	}
	if modalWidth < 40 {
		modalWidth = screenWidth - 4
	}

	contentWidth := modalWidth - 4
	border := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
	lines := []string{
		border.Render("╭" + strings.Repeat("─", modalWidth-2) + "╮"),
		border.Render("│") + " " + strings.Repeat(" ", contentWidth) + " " + border.Render("│"),
	}
	for _, line := range strings.Split(body, "\n") {
		lines = append(lines, border.Render("│")+" "+fitLine(line, contentWidth)+" "+border.Render("│"))
	}
	lines = append(lines,
		border.Render("│")+" "+strings.Repeat(" ", contentWidth)+" "+border.Render("│"),
		border.Render("╰"+strings.Repeat("─", modalWidth-2)+"╯"),
	)
	return ModalView{
		content: strings.Join(lines, "\n"),
		width:   modalWidth,
	}
}

func centerLine(line string, width int) string {
	lineWidth := lipgloss.Width(line)
	if lineWidth >= width {
		return line
	}
	left := (width - lineWidth) / 2
	right := width - lineWidth - left
	return strings.Repeat(" ", left) + line + strings.Repeat(" ", right)
}

func fitLine(line string, width int) string {
	if lipgloss.Width(line) > width {
		line = truncateANSI(line, width)
	}
	padding := width - lipgloss.Width(line)
	if padding > 0 {
		line += strings.Repeat(" ", padding)
	}
	return line
}

func truncateANSI(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}

	limit := max
	suffix := ""
	if max > 3 {
		limit = max - 3
		suffix = "..."
	}

	var b strings.Builder
	visible := 0
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			j := i + 1
			if j < len(s) && s[j] == '[' {
				j++
				for j < len(s) {
					c := s[j]
					j++
					if c >= '@' && c <= '~' {
						break
					}
				}
				b.WriteString(s[i:j])
				i = j
				continue
			}
		}

		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 0 {
			break
		}
		if visible >= limit {
			break
		}
		b.WriteRune(r)
		visible++
		i += size
	}
	if suffix != "" {
		b.WriteString("\x1b[0m")
		b.WriteString(suffix)
	}
	return b.String()
}

func listDimensions(width, height, maxHeight int) (int, int) {
	listWidth := width - 4
	if listWidth < 20 {
		listWidth = 20
	}

	listHeight := height - 8
	if listHeight > maxHeight {
		listHeight = maxHeight
	}
	if listHeight < 5 {
		listHeight = 5
	}
	return listWidth, listHeight
}

func formatTunnelAddr(addr string) string {
	if addr == "" {
		return "-"
	}
	if strings.Contains(addr, ":") {
		return addr
	}
	return ":" + addr
}

func validPortInput(input string, allowAddr bool) bool {
	if input == "" {
		return false
	}
	if !strings.Contains(input, ":") {
		return validPort(input)
	}
	if !allowAddr {
		return false
	}
	host, port, err := net.SplitHostPort(input)
	if err != nil || host == "" {
		return false
	}
	return validPort(port)
}

func validPortInputRune(r rune, allowAddr bool) bool {
	if r >= '0' && r <= '9' {
		return true
	}
	return allowAddr && (r == '.' || r == ':')
}

func validPort(port string) bool {
	n, err := strconv.Atoi(port)
	return err == nil && n > 0 && n <= 65535
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
	return fmt.Sprintf("%s%s/s", dir, formatBytes(bytes))
}

func formatTotal(bytes int64, dir string) string {
	return fmt.Sprintf("%s%s", dir, formatBytes(bytes))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
