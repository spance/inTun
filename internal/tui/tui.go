package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	sftplib "github.com/pkg/sftp"
	"github.com/spance/intun/internal/config"
	"github.com/spance/intun/internal/platform"
	"github.com/spance/intun/internal/sftp"
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
	ScreenSFTP
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
	statusMsg     string
	statusTicks   int
	quitConfirm   bool
	authQueue     *AuthPromptQueue
	promptMode    bool
	promptInput   string
	authCtx       *platform.AuthContext
	cancelCtx     context.Context
	cancelFunc    context.CancelFunc

	sftpClient       *sftp.Client
	sftpLocalDir     string
	sftpRemoteDir    string
	sftpLocalFiles   []sftp.FileEntry
	sftpRemoteFiles  []sftp.FileEntry
	sftpFocus        int
	sftpCursor       [2]int
	sftpScroll       [2]int
	sftpTransferring bool
	sftpProgress     *sftp.ProgressInfo
	sftpPreview      string
	sftpPreviewing   bool
	sftpCancel       context.CancelFunc
	sftpTunnelID     int
	sftpHostLabel    string
	sftpDone         chan struct{}
	sftpDirection    string
	sftpPrevDone     int64
	sftpRenaming       bool
	sftpRenameInput    string
	sftpSyncConfirm    bool
	sftpSyncConfirmMsg string
}

func (m *Model) setStatusMsg(msg string) {
	m.statusMsg = msg
	m.statusTicks = 3
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
		if listHeight > hostListHeight {
			listHeight = hostListHeight
		}
		if listHeight < 5 {
			listHeight = 5
		}
		m.hostList.SetSize(msg.Width-4, listHeight)
		m.typeList.SetSize(msg.Width-4, min(typeListHeight, msg.Height-8))
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
		if m.statusTicks > 0 {
			m.statusTicks--
			if m.statusTicks == 0 {
				m.statusMsg = ""
			}
		}
		if m.screen == ScreenSFTP && m.sftpDone != nil {
			if m.sftpTransferring && m.sftpProgress != nil {
				m.sftpProgress.Speed = (m.sftpProgress.Done - m.sftpPrevDone) * 2
				m.sftpPrevDone = m.sftpProgress.Done
			}
			select {
			case <-m.sftpDone:
				m.sftpTransferring = false
				if m.sftpProgress != nil {
					m.sftpProgress.Active = false
				}
				m = m.refreshSFTPFiles()
				m.sftpDone = nil
			default:
			}
		}
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

	if m.screen == ScreenMain && msg.String() != "q" && msg.String() != "ctrl+c" {
		m.quitConfirm = false
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
	case ScreenSFTP:
		return m.handleSFTPKeys(msg)
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
		} else {
			m.setStatusMsg("No tunnel selected")
		}
	case "s":
		if len(tunnels) > 0 && m.selectedIndex < len(tunnels) {
			t := tunnels[m.selectedIndex]
			if t.Status == tunnel.StatusRunning {
				m.manager.Stop(t.ID)
			} else if t.Status == tunnel.StatusStopped {
				m.manager.Restart(t.ID)
			} else if t.Status == tunnel.StatusConnecting {
				m.setStatusMsg("Cannot stop: tunnel is connecting")
			} else if t.Status == tunnel.StatusError {
				m.setStatusMsg("Use [r] to reconnect")
			}
		} else {
			m.setStatusMsg("No tunnel selected")
		}
	case "d":
		if len(tunnels) > 0 && m.selectedIndex < len(tunnels) {
			m.manager.Delete(tunnels[m.selectedIndex].ID)
			if m.selectedIndex > 0 {
				m.selectedIndex--
			}
		} else {
			m.setStatusMsg("No tunnel selected")
		}
	case "f":
		if len(tunnels) > 0 && m.selectedIndex < len(tunnels) {
			t := tunnels[m.selectedIndex]
			if t.Status == tunnel.StatusRunning {
				rawClient, err := m.manager.GetSFTPClient(t.ID)
				if err != nil {
					m.err = err
					return m, nil
				}
				sftpRaw, ok := rawClient.(*sftplib.Client)
				if !ok {
					m.err = fmt.Errorf("invalid SFTP client type")
					return m, nil
				}
				m.sftpClient = sftp.NewClient(sftpRaw)
				cwd, _ := os.Getwd()
				m.sftpLocalDir = cwd
				remoteDir, _ := sftpRaw.Getwd()
				if remoteDir == "" {
					remoteDir = "/"
				}
				m.sftpRemoteDir = remoteDir
				localFiles, lerr := sftp.ReadLocalDir(m.sftpLocalDir)
				if lerr != nil {
					m.err = lerr
					m.sftpClient = nil
					return m, nil
				}
				remoteFiles, rerr := m.sftpClient.ReadRemoteDir(m.sftpRemoteDir)
				if rerr != nil {
					m.err = rerr
					m.sftpClient = nil
					return m, nil
				}
				m.sftpLocalFiles = localFiles
				m.sftpRemoteFiles = remoteFiles
				m.sftpFocus = 0
				m.sftpCursor = [2]int{0, 0}
				m.sftpScroll = [2]int{0, 0}
				m.sftpTransferring = false
				m.sftpPreview = ""
				m.sftpPreviewing = false
				m.sftpCancel = nil
				m.sftpTunnelID = t.ID
				m.sftpHostLabel = fmt.Sprintf("%s@%s:%s", t.SSHConfig.User, t.SSHConfig.Host, t.SSHConfig.Port)
				m.sftpDone = nil
				m.sftpDirection = ""
				m.screen = ScreenSFTP
			} else {
				m.setStatusMsg("Tunnel must be running to use SFTP")
			}
		} else {
			m.setStatusMsg("No tunnel selected")
		}
	case "up", "k":
		if m.selectedIndex > 0 {
			m.selectedIndex--
		}
	case "down", "j":
		if m.selectedIndex < len(tunnels)-1 {
			m.selectedIndex++
		}
	case "q", "ctrl+c":
		if m.quitConfirm {
			return m, tea.Quit
		}
		tunnels := m.manager.List()
		for _, t := range tunnels {
			if t.Status == tunnel.StatusRunning {
				m.quitConfirm = true
				return m, nil
			}
		}
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
			m.localPort = m.portInput
			m.manager.Create(m.selectedHost.Name, m.buildSSHConfig(), m.selectedType, m.localPort, "")
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
			m.localPort = m.portInput
			m.portInput = ""
			m.inputMode = 1
			return m, nil
		}
		m.remotePort = m.portInput
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
		ch := msg.String()
		if len(ch) == 1 {
			c := ch[0]
			if m.selectedType == tunnel.Remote {
				if (c >= '0' && c <= '9') || c == '.' || c == ':' {
					m.portInput += ch
				}
			} else if c >= '0' && c <= '9' {
				m.portInput += ch
			}
		}
	}
	return m, nil
}

func (m Model) View() string {
	var b strings.Builder

	width := m.width
	if 	width < minTermWidth {
		width = 80
	}

	var title string
	if m.screen == ScreenSFTP {
		title = fmt.Sprintf(" inTun SFTP - %s", m.sftpHostLabel)
	} else {
		title = fmt.Sprintf(" inTun - Interactive SSH Tunnel (%s)", m.version)
	}
	titlePadding := width - lipgloss.Width(title)
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
	case ScreenSFTP:
		b.WriteString(m.renderSFTPScreen())
	}

	content := b.String()
	lines := strings.Count(content, "\n")
	remainingLines := m.height - lines - 1
	if remainingLines > 0 {
		content += strings.Repeat("\n", remainingLines)
	}

	content += m.renderShortcuts()

	if overlay := m.renderStatusOverlay(width, m.height); overlay != "" {
		contentLines := strings.Split(content, "\n")
		overlayLines := strings.Split(overlay, "\n")
		maxLines := len(contentLines)
		if len(overlayLines) > maxLines {
			maxLines = len(overlayLines)
		}
		var sb strings.Builder
		for i := 0; i < maxLines; i++ {
			useOverlay := i < len(overlayLines) && strings.Contains(overlayLines[i], "\x1b[")
			if useOverlay {
				sb.WriteString(overlayLines[i])
			} else if i < len(contentLines) {
				line := contentLines[i]
				lineW := lipgloss.Width(line)
				if lineW < width {
					line += strings.Repeat(" ", width-lineW)
				}
				sb.WriteString(line)
			} else {
				sb.WriteString(strings.Repeat(" ", width))
			}
			if i < maxLines-1 {
				sb.WriteString("\n")
			}
		}
		content = sb.String()
	}

	return content
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
	if 	lineWidth < minTermWidth {
		lineWidth = 80
	}

	separator := strings.Repeat("=", lineWidth)

	fixedWidth := colFixedWidth
	nameWidth := lineWidth - fixedWidth
	if nameWidth < 10 {
		nameWidth = 10
	}

	header := fmt.Sprintf("  %-4s %-*s   %-13s %-10s %-21s %-21s %-8s",
		"#", nameWidth, "Name", "Status", "Type", "Local", "Remote", "Latency")
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
		badgeW := lipgloss.Width(badge)
		badgePad := ""
		if badgeW < colStatusW {
			badgePad = strings.Repeat(" ", colStatusW-badgeW)
		}

		if i == m.selectedIndex {
			plainPart := fmt.Sprintf("%s%-4d %-*s   ", prefix, t.ID, nameWidth, truncate(t.Name, nameWidth))
			afterBadge := fmt.Sprintf(" %-10s %-21s %-21s %-8s",
				t.Type.String(), ":"+t.LocalPort, remote, latency)
			b.WriteString(selectedStyle.Render(plainPart))
			b.WriteString(badge)
			b.WriteString(badgePad)
			b.WriteString(selectedStyle.Render(afterBadge))
		} else {
			line := fmt.Sprintf("%s%-4d %-*s   %s%s %-10s %-21s %-21s %-8s",
				prefix, t.ID, nameWidth, truncate(t.Name, nameWidth), badge, badgePad,
				t.Type.String(), ":"+t.LocalPort, remote, latency)
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
		b.WriteString(fmt.Sprintf("[%s]\n\n", strings.Repeat("*", len([]rune(m.promptInput)))))
		b.WriteString(shortcutStyle.Render("[Enter] Submit  [Esc] Cancel"))
	}

	return b.String()
}

func (m Model) renderStatusOverlay(width, height int) string {
	if m.sftpSyncConfirm {
		return renderDialogBox(width, height, m.sftpSyncConfirmMsg, true)
	}
	if m.statusMsg == "" && !m.quitConfirm {
		return ""
	}
	msg := m.statusMsg
	if m.quitConfirm {
		msg = "Active tunnels running. Press q again to quit."
	}
	return renderDialogBox(width, height, msg, false)
}

func renderDialogBox(width, height int, msg string, hasButtons bool) string {
	boxBg := "\x1b[48;5;236m"
	textFg := "\x1b[38;5;252m"
	labelFg := "\x1b[38;2;166;227;161m\x1b[1m"
	btnBg := "\x1b[48;5;75m\x1b[38;5;235m\x1b[1m"
	dimFg := "\x1b[38;5;102m"
	reset := "\x1b[0m"

	msgLines := strings.Split(msg, "\n")

	padX := 3
	padY := 1
	rawLines := make([]string, 0, len(msgLines)+padY*2+2)
	for i := 0; i < padY; i++ {
		rawLines = append(rawLines, "")
	}
	for _, ml := range msgLines {
		rawLines = append(rawLines, ml)
	}
	if hasButtons {
		rawLines = append(rawLines, "")
		rawLines = append(rawLines, "confirm cancel")
	}
	for i := 0; i < padY; i++ {
		rawLines = append(rawLines, "")
	}

	maxRawW := 0
	for _, rl := range rawLines {
		if len(rl) > maxRawW {
			maxRawW = len(rl)
		}
	}
	innerW := maxRawW + padX*2

	styledLines := make([]string, len(rawLines))
	for i, rl := range rawLines {
		padL := padX
		padR := innerW - len(rl) - padX
		if padR < padX {
			padR = padX
		}

		if rl == "" {
			styledLines[i] = boxBg + strings.Repeat(" ", innerW) + reset
		} else if strings.HasPrefix(rl, "FROM:") || strings.HasPrefix(rl, "TO:") {
			parts := strings.SplitN(rl, ": ", 2)
			label := parts[0] + ":"
			value := ""
			if len(parts) == 2 {
				value = parts[1]
			}
			content := labelFg + label + reset + " " + textFg + value + reset
			padR = innerW - padX - len(label) - 1 - len(value) - padX
			if padR < 0 {
				padR = 0
			}
			styledLines[i] = boxBg + strings.Repeat(" ", padL) + content + boxBg + strings.Repeat(" ", padR) + reset
		} else if rl == "confirm cancel" {
			confirmStr := btnBg + " Enter Confirm " + reset
			cancelStr := boxBg + dimFg + "  Esc Cancel" + boxBg + strings.Repeat(" ", padX) + reset
			btnContent := confirmStr + cancelStr
			styledLines[i] = boxBg + strings.Repeat(" ", padL) + btnContent + boxBg + strings.Repeat(" ", padR) + reset
		} else {
			padR = innerW - padX - len(rl) - padX
			if padR < 0 {
				padR = 0
			}
			styledLines[i] = boxBg + strings.Repeat(" ", padL) + textFg + "\x1b[1m" + rl + reset + boxBg + strings.Repeat(" ", padR) + reset
		}
	}

	boxW := lipgloss.Width(styledLines[0])
	boxH := len(styledLines)
	row := height/2 - boxH/2
	col := (width - boxW) / 2

	var b strings.Builder
	for y := 0; y < height; y++ {
		if y == row {
			for bi, sl := range styledLines {
				if col > 0 {
					b.WriteString(strings.Repeat(" ", col))
				}
				b.WriteString(sl)
				if bi < len(styledLines)-1 {
					b.WriteString("\n")
				}
			}
			y += boxH - 1
			continue
		}
		if y < height-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m Model) renderShortcuts() string {
	width := m.width
	if 	width < minTermWidth {
		width = 80
	}

	var items []string
	switch m.screen {
	case ScreenMain:
		items = []string{
			"[" + keyStyle.Render("↑↓") + "]Navigate",
			"[" + keyStyle.Render("c") + "]Create",
			"[" + keyStyle.Render("f") + "]SFTP",
			"[" + keyStyle.Render("r") + "]Reconnect",
			"[" + keyStyle.Render("s") + "]Stop/Start",
			"[" + keyStyle.Render("d") + "]Delete",
			"[" + keyStyle.Render("q") + "]Quit",
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
	case ScreenSFTP:
		if m.sftpPreviewing {
			items = []string{
				"[" + keyStyle.Render("Esc") + "]Close Preview",
			}
		} else if m.sftpTransferring {
			items = []string{
				"[" + keyStyle.Render("Tab") + "]Switch",
				"[" + keyStyle.Render("↑↓") + "]Navigate",
				"Transferring...",
				"[" + keyStyle.Render("q") + "]Back",
			}
		} else {
			items = []string{
				"[" + keyStyle.Render("Tab") + "]Switch",
				"[" + keyStyle.Render("↑↓") + "]Navigate",
				"[" + keyStyle.Render("Enter") + "]Open",
				"[" + keyStyle.Render("s") + "]Sync",
				"[" + keyStyle.Render("r") + "]Sync Dir",
				"[" + keyStyle.Render("n") + "]Rename",
				"[" + keyStyle.Render("v") + "]Preview",
				"[" + keyStyle.Render("q") + "]Back",
			}
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
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

func formatBytes(b int64) string {
	const KB = 1024
	const MB = KB * 1024

	if b >= MB {
		return fmt.Sprintf("%.2f MB", float64(b)/float64(MB))
	}
	return fmt.Sprintf("%.2f KB", float64(b)/float64(KB))
}

func formatTransferSpeed(bytesPerSec int64) string {
	if bytesPerSec >= 1024*1024 {
		return fmt.Sprintf("%.1fMB/s", float64(bytesPerSec)/(1024*1024))
	}
	return fmt.Sprintf("%.1fKB/s", float64(bytesPerSec)/1024)
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

func (m Model) handleSFTPKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.sftpRenaming {
		switch msg.String() {
		case "enter":
			return m.sftpConfirmRename()
		case "esc":
			m.sftpRenaming = false
			m.sftpRenameInput = ""
		case "backspace":
			if len(m.sftpRenameInput) > 0 {
				m.sftpRenameInput = m.sftpRenameInput[:len(m.sftpRenameInput)-1]
			}
		default:
			if len(msg.String()) == 1 && msg.String()[0] >= 32 {
				m.sftpRenameInput += msg.String()
			}
		}
		return m, nil
	}

	if m.sftpSyncConfirm {
		switch msg.String() {
		case "enter":
			m.sftpSyncConfirm = false
			m.sftpSyncConfirmMsg = ""
			return m.sftpDoRecursive()
		case "esc":
			m.sftpSyncConfirm = false
			m.sftpSyncConfirmMsg = ""
		}
		return m, nil
	}

	if m.sftpPreviewing {
		switch msg.String() {
		case "esc", "q":
			m.sftpPreviewing = false
			m.sftpPreview = ""
		}
		return m, nil
	}

	switch msg.String() {
	case "q", "esc":
		if m.sftpCancel != nil {
			m.sftpCancel()
			m.sftpCancel = nil
		}
		if m.sftpClient != nil {
			m.sftpClient.Close()
			m.sftpClient = nil
		}
		m.screen = ScreenMain
		return m, nil
	case "tab":
		if m.sftpFocus == 0 {
			m.sftpFocus = 1
		} else {
			m.sftpFocus = 0
		}
	case "up", "k":
		if m.sftpCursor[m.sftpFocus] > 0 {
			m.sftpCursor[m.sftpFocus]--
		}
	case "down", "j":
		files := m.currentSFTPFiles()
		if m.sftpCursor[m.sftpFocus] < len(files) {
			m.sftpCursor[m.sftpFocus]++
		}
	case "pgup":
		visibleHeight := m.sftpVisibleHeight()
		cur := &m.sftpCursor[m.sftpFocus]
		*cur -= visibleHeight
		if *cur < 0 {
			*cur = 0
		}
	case "pgdown":
		files := m.currentSFTPFiles()
		visibleHeight := m.sftpVisibleHeight()
		maxCur := len(files)
		cur := &m.sftpCursor[m.sftpFocus]
		*cur += visibleHeight
		if *cur > maxCur {
			*cur = maxCur
		}
	case "enter":
		return m.sftpEnterDir()
	case "s":
		if m.sftpTransferring {
			m.setStatusMsg("Wait for transfer to complete")
			return m, nil
		}
		return m.sftpStartSync()
	case "r":
		if m.sftpTransferring {
			m.setStatusMsg("Wait for transfer to complete")
			return m, nil
		}
		return m.sftpStartRecursiveConfirm()
	case "v":
		if m.sftpTransferring {
			m.setStatusMsg("Wait for transfer to complete")
			return m, nil
		}
		return m.sftpPreviewFile()
	case "n":
		if !m.sftpTransferring {
			files := m.currentSFTPFiles()
			cursor := m.sftpCursor[m.sftpFocus]
			if cursor == 0 || cursor > len(files) {
				m.setStatusMsg("No file selected")
				return m, nil
			}
			m.sftpRenaming = true
			m.sftpRenameInput = files[cursor-1].Name
		} else {
			m.setStatusMsg("Wait for transfer to complete")
		}
	}
	return m, nil
}

func (m Model) currentSFTPFiles() []sftp.FileEntry {
	if m.sftpFocus == 0 {
		return m.sftpLocalFiles
	}
	return m.sftpRemoteFiles
}

func (m Model) sftpVisibleHeight() int {
	h := m.height - 8
	if h < 5 {
		h = 5
	}
	return h
}

func (m Model) sftpEnterDir() (tea.Model, tea.Cmd) {
	files := m.currentSFTPFiles()
	cursor := m.sftpCursor[m.sftpFocus]
	var target string

	if cursor == 0 {
		if m.sftpFocus == 0 {
			target = filepath.Dir(m.sftpLocalDir)
		} else {
			target = filepath.Dir(m.sftpRemoteDir)
		}
	} else {
		idx := cursor - 1
		if idx >= len(files) || !files[idx].IsDir {
			m.setStatusMsg("Use [v] to preview, [s] to sync")
			return m, nil
		}
		if m.sftpFocus == 0 {
			target = filepath.Join(m.sftpLocalDir, files[idx].Name)
		} else {
			if m.sftpRemoteDir == "/" {
				target = "/" + files[idx].Name
			} else {
				target = m.sftpRemoteDir + "/" + files[idx].Name
			}
		}
	}

	return m.sftpNavigateTo(target)
}

func (m Model) sftpNavigateTo(path string) (tea.Model, tea.Cmd) {
	if m.sftpFocus == 0 {
		localFiles, err := sftp.ReadLocalDir(path)
		if err != nil {
			m.setStatusMsg("Cannot open directory")
			return m, nil
		}
		m.sftpLocalDir = path
		m.sftpLocalFiles = localFiles
	} else {
		remoteFiles, err := m.sftpClient.ReadRemoteDir(path)
		if err != nil {
			m.setStatusMsg("Cannot open directory")
			return m, nil
		}
		m.sftpRemoteDir = path
		m.sftpRemoteFiles = remoteFiles
	}
	m.sftpCursor[m.sftpFocus] = 0
	m.sftpScroll[m.sftpFocus] = 0
	return m, nil
}

func (m Model) sftpStartSync() (tea.Model, tea.Cmd) {
	if m.sftpFocus == 0 {
		files := m.sftpLocalFiles
		cursor := m.sftpCursor[0]
		if cursor == 0 || cursor > len(files) {
			m.setStatusMsg("No file selected")
			return m, nil
		}
		entry := files[cursor-1]
		if entry.IsDir {
			m.setStatusMsg("Use [r] for directory sync")
			return m, nil
		}
		localPath := filepath.Join(m.sftpLocalDir, entry.Name)
		remotePath := m.sftpRemoteDir + "/" + entry.Name
		m.sftpTransferring = true
		m.sftpDirection = "↑"
		m.sftpProgress = &sftp.ProgressInfo{File: entry.Name, Total: entry.Size, Active: true}
		m.sftpPrevDone = 0
		done := make(chan struct{})
		m.sftpDone = done
		client := m.sftpClient
		go func() {
			client.Upload(localPath, remotePath, func(n int64) {
				m.sftpProgress.Done = n
			})
			close(done)
		}()
		return m, nil
	}

	files := m.sftpRemoteFiles
	cursor := m.sftpCursor[1]
	if cursor == 0 || cursor > len(files) {
		m.setStatusMsg("No file selected")
		return m, nil
	}
	entry := files[cursor-1]
	if entry.IsDir {
		m.setStatusMsg("Use [r] for directory sync")
		return m, nil
	}
	remotePath := m.sftpRemoteDir + "/" + entry.Name
	localPath := filepath.Join(m.sftpLocalDir, entry.Name)
	m.sftpTransferring = true
	m.sftpDirection = "↓"
	m.sftpProgress = &sftp.ProgressInfo{File: entry.Name, Total: entry.Size, Active: true}
	m.sftpPrevDone = 0
	done := make(chan struct{})
	m.sftpDone = done
	client := m.sftpClient
	go func() {
		client.Download(remotePath, localPath, func(n int64) {
			m.sftpProgress.Done = n
		})
		close(done)
	}()
	return m, nil
}

func (m Model) sftpStartRecursiveConfirm() (tea.Model, tea.Cmd) {
	files := m.currentSFTPFiles()
	cursor := m.sftpCursor[m.sftpFocus]
	if cursor == 0 || cursor > len(files) {
		m.setStatusMsg("No file selected")
		return m, nil
	}
	entry := files[cursor-1]
	if !entry.IsDir {
		m.setStatusMsg("Use [s] for file sync")
		return m, nil
	}

	var src, dst string
	if m.sftpFocus == 0 {
		src = filepath.Join(m.sftpLocalDir, entry.Name)
		dst = m.sftpRemoteDir + "/" + entry.Name
	} else {
		src = m.sftpRemoteDir + "/" + entry.Name
		dst = filepath.Join(m.sftpLocalDir, entry.Name)
	}

	m.sftpSyncConfirm = true
	m.sftpSyncConfirmMsg = fmt.Sprintf("FROM: %s\nTO:   %s", src, dst)
	return m, nil
}

func (m Model) sftpDoRecursive() (tea.Model, tea.Cmd) {
	if m.sftpFocus == 0 {
		files := m.sftpLocalFiles
		cursor := m.sftpCursor[0]
		if cursor == 0 || cursor > len(files) {
			return m, nil
		}
		entry := files[cursor-1]
		if !entry.IsDir {
			return m, nil
		}
		localPath := filepath.Join(m.sftpLocalDir, entry.Name)
		remotePath := m.sftpRemoteDir + "/" + entry.Name
		m.sftpTransferring = true
		m.sftpDirection = "⇡"
		m.sftpProgress = &sftp.ProgressInfo{File: entry.Name, Active: true}
		m.sftpPrevDone = 0
		done := make(chan struct{})
		m.sftpDone = done
		client := m.sftpClient
		go func() {
			client.UploadDir(localPath, remotePath, func(done, total int64, file string) {
				m.sftpProgress.Done = done
				m.sftpProgress.Total = total
				m.sftpProgress.File = file
			})
			close(done)
		}()
		return m, nil
	}

	files := m.sftpRemoteFiles
	cursor := m.sftpCursor[1]
	if cursor == 0 || cursor > len(files) {
		return m, nil
	}
	entry := files[cursor-1]
	if !entry.IsDir {
		return m, nil
	}
	remotePath := m.sftpRemoteDir + "/" + entry.Name
	localPath := filepath.Join(m.sftpLocalDir + "/" + entry.Name)
	m.sftpTransferring = true
	m.sftpDirection = "⇣"
	m.sftpProgress = &sftp.ProgressInfo{File: entry.Name, Active: true}
	m.sftpPrevDone = 0
	done := make(chan struct{})
	m.sftpDone = done
	client := m.sftpClient
	go func() {
		client.DownloadDir(remotePath, localPath, func(done, total int64, file string) {
			m.sftpProgress.Done = done
			m.sftpProgress.Total = total
			m.sftpProgress.File = file
		})
		close(done)
	}()
	return m, nil
}

func (m Model) sftpPreviewFile() (tea.Model, tea.Cmd) {
	files := m.currentSFTPFiles()
	cursor := m.sftpCursor[m.sftpFocus]
	if cursor == 0 || cursor > len(files) {
		m.setStatusMsg("No file selected")
		return m, nil
	}
	entry := files[cursor-1]
	if entry.IsDir {
		m.setStatusMsg("Cannot preview a directory")
		return m, nil
	}

	if m.sftpFocus == 0 {
		localPath := filepath.Join(m.sftpLocalDir, entry.Name)
		data, err := os.ReadFile(localPath)
		if err != nil {
			m.sftpPreview = fmt.Sprintf("Error: %v", err)
			m.sftpPreviewing = true
			return m, nil
		}
		if len(data) > 4096 {
			data = data[:4096]
		}
		content := string(data)
		if isBinaryContent(content) {
			content = "[binary file]"
		}
		m.sftpPreview = content
	} else {
		remotePath := m.sftpRemoteDir + "/" + entry.Name
		content, err := m.sftpClient.Preview(remotePath)
		if err != nil {
			m.sftpPreview = fmt.Sprintf("Error: %v", err)
			m.sftpPreviewing = true
			return m, nil
		}
		m.sftpPreview = content
	}
	m.sftpPreviewing = true
	return m, nil
}

func (m Model) sftpConfirmRename() (tea.Model, tea.Cmd) {
	files := m.currentSFTPFiles()
	cursor := m.sftpCursor[m.sftpFocus]
	if cursor == 0 || cursor > len(files) {
		m.sftpRenaming = false
		m.sftpRenameInput = ""
		return m, nil
	}

	oldName := files[cursor-1].Name
	newName := m.sftpRenameInput
	m.sftpRenaming = false
	m.sftpRenameInput = ""

	if newName == "" || newName == oldName {
		return m, nil
	}

	if m.sftpFocus == 0 {
		oldPath := filepath.Join(m.sftpLocalDir, oldName)
		newPath := filepath.Join(m.sftpLocalDir, newName)
		if err := os.Rename(oldPath, newPath); err != nil {
			m.setStatusMsg(fmt.Sprintf("Rename failed: %v", err))
			return m, nil
		}
	} else {
		oldPath := m.sftpRemoteDir + "/" + oldName
		newPath := m.sftpRemoteDir + "/" + newName
		if err := m.sftpClient.Rename(oldPath, newPath); err != nil {
			m.setStatusMsg(fmt.Sprintf("Rename failed: %v", err))
			return m, nil
		}
	}

	m = m.refreshSFTPFiles()
	return m, nil
}

func (m Model) refreshSFTPFiles() Model {
	localFiles, err := sftp.ReadLocalDir(m.sftpLocalDir)
	if err == nil {
		m.sftpLocalFiles = localFiles
	}
	remoteFiles, err := m.sftpClient.ReadRemoteDir(m.sftpRemoteDir)
	if err == nil {
		m.sftpRemoteFiles = remoteFiles
	}
	return m
}

func isBinaryContent(s string) bool {
	for _, r := range s {
		if r == 0 {
			return true
		}
	}
	return false
}

func (m Model) renderSFTPScreen() string {
	width := m.width
	if width < minTermWidth {
		width = 80
	}
	panelWidth := width/2 - 3
	if panelWidth < 20 {
		panelWidth = 20
	}

	var b strings.Builder

	localPanel := m.renderSFTPPanel("LOCAL", m.sftpLocalDir, m.sftpLocalFiles, 0, panelWidth, m.sftpFocus == 0)
	remotePanel := m.renderSFTPPanel("REMOTE", m.sftpRemoteDir, m.sftpRemoteFiles, 1, panelWidth, m.sftpFocus == 1)

	localLines := strings.Split(localPanel, "\n")
	remoteLines := strings.Split(remotePanel, "\n")
	maxLines := len(localLines)
	if len(remoteLines) > maxLines {
		maxLines = len(remoteLines)
	}

	sep := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render("│")
	for i := 0; i < maxLines; i++ {
		var left, right string
		if i < len(localLines) {
			left = localLines[i]
		}
		if i < len(remoteLines) {
			right = remoteLines[i]
		}
		leftPad := panelWidth - lipgloss.Width(left)
		if leftPad < 0 {
			leftPad = 0
		}
		rightPad := panelWidth - lipgloss.Width(right)
		if rightPad < 0 {
			rightPad = 0
		}
		b.WriteString(left)
		b.WriteString(strings.Repeat(" ", leftPad))
		b.WriteString(" ")
		b.WriteString(sep)
		b.WriteString(" ")
		b.WriteString(right)
		b.WriteString(strings.Repeat(" ", rightPad))
		b.WriteString("\n")
	}

	if m.sftpRenaming {
		renameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
		input := m.sftpRenameInput + "_"
		hint := "Rename: " + input
		confirmHint := "  [Enter]Confirm [Esc]Cancel"
		rendered := renameStyle.Render(hint) + shortcutStyle.Render(confirmHint)
		pad := width - lipgloss.Width(rendered)
		if pad > 0 {
			rendered += strings.Repeat(" ", pad)
		}
		b.WriteString(rendered)
		b.WriteString("\n")
	}

	if m.sftpTransferring {
		b.WriteString(m.renderSFTPProgress(panelWidth))
		b.WriteString("\n")
	}

	if m.sftpPreviewing {
		b.WriteString(m.renderSFTPPreview(width))
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderSFTPPanel(label, dir string, files []sftp.FileEntry, panelIdx, width int, focused bool) string {
	var b strings.Builder

	hdrStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Bold(true)
	if focused {
		hdrStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED")).Bold(true)
	}

	dirDisplay := truncate(dir, width-len(label)-2)
	hdrText := fmt.Sprintf("%s: %s", label, dirDisplay)
	visibleW := lipgloss.Width(hdrText)
	padW := width - visibleW
	if padW > 0 {
		hdrText += strings.Repeat(" ", padW)
	}
	b.WriteString(hdrStyle.Render(hdrText))
	b.WriteString("\n")

	visibleHeight := m.height - 8
	if visibleHeight < 5 {
		visibleHeight = 5
	}

	cursor := m.sftpCursor[panelIdx]
	scroll := m.sftpScroll[panelIdx]
	if cursor < scroll {
		scroll = cursor
	}
	if cursor > scroll+visibleHeight-1 {
		scroll = cursor - visibleHeight + 1
	}

	totalEntries := len(files) + 1
	renderIdx := 0
	for i := scroll; i < totalEntries && renderIdx < visibleHeight; i++ {
		var name, sizeStr string
		var isDir bool

		if i == 0 {
			name = ".."
			isDir = true
		} else {
			idx := i - 1
			if idx >= len(files) {
				break
			}
			entry := files[idx]
			name = entry.Name
			isDir = entry.IsDir
			if !isDir && entry.Size > 0 {
				sizeStr = formatBytes(entry.Size)
			}
		}

		displayName := name
		if isDir && i > 0 {
			displayName = name + "/"
		}

		prefix := "  "
		if i == cursor {
			prefix = "▸ "
		}

		var line string
		if sizeStr != "" {
			nameWidth := width - 3 - len(sizeStr)
			if nameWidth < 5 {
				nameWidth = 5
			}
			truncated := truncate(displayName, nameWidth)
			padLen := width - 3 - len(truncated) - len(sizeStr)
			if padLen < 1 {
				padLen = 1
			}
			line = prefix + truncated + strings.Repeat(" ", padLen) + sizeStr
		} else {
			line = prefix + truncate(displayName, width-3)
		}

		if i == cursor && focused {
			linePad := width - lipgloss.Width(line)
			if linePad > 0 {
				line += strings.Repeat(" ", linePad)
			}
			b.WriteString(selectedStyle.Render(line))
		} else if i == cursor {
			linePad := width - lipgloss.Width(line)
			if linePad > 0 {
				line += strings.Repeat(" ", linePad)
			}
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF")).Render(line))
		} else {
			linePad := width - lipgloss.Width(line)
			if linePad > 0 {
				line += strings.Repeat(" ", linePad)
			}
			b.WriteString(shortcutStyle.Render(line))
		}
		b.WriteString("\n")
		renderIdx++
	}

	return b.String()
}

func (m Model) renderSFTPPreview(width int) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#7C3AED")).Bold(true).Render("── Preview ──"))
	b.WriteString("\n")

	lines := strings.Split(m.sftpPreview, "\n")
	maxLines := 20
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}

	previewStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#D1D5DB")).Width(width - 4)
	for _, line := range lines {
		displayLine := truncate(line, width-4)
		b.WriteString(previewStyle.Render(displayLine))
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderSFTPProgress(width int) string {
	p := m.sftpProgress
	if p == nil {
		return ""
	}
	var pct float64
	if p.Total > 0 {
		pct = float64(p.Done) / float64(p.Total) * 100
	}
	barWidth := width / 4
	if barWidth < 10 {
		barWidth = 10
	}
	filled := int(pct / 100 * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	var speedStr string
	if p.Speed > 0 {
		speedStr = " " + formatTransferSpeed(p.Speed)
	}

	progressStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
	return progressStyle.Render(fmt.Sprintf("%s %s [%s] %.0f%%%s", m.sftpDirection, p.File, bar, pct, speedStr))
}
