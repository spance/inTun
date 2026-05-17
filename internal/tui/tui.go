package tui

import (
	"context"
	"fmt"
	"image/color"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	colorSurface  = lipgloss.Color("#161B22")
	colorPanel    = lipgloss.Color("#0D1117")
	colorPanelHi  = lipgloss.Color("#1F2937")
	colorGlass    = lipgloss.Color("#111827")
	colorText     = lipgloss.Color("#D6DEEB")
	colorMuted    = lipgloss.Color("#7D8590")
	colorAccent   = lipgloss.Color("#56B6C2")
	colorAccent2  = lipgloss.Color("#7C3AED")
	colorSuccess  = lipgloss.Color("#3FB950")
	colorWarning  = lipgloss.Color("#D29922")
	colorDanger   = lipgloss.Color("#F85149")
	colorSelected = lipgloss.Color("#E6EDF3")

	titleStyle = lipgloss.NewStyle().
			Foreground(colorSelected).
			Background(colorAccent2).
			Bold(true).
			Padding(0, 1)

	statusStyle = lipgloss.NewStyle().
			Foreground(colorSuccess)

	errorStyle = lipgloss.NewStyle().
			Foreground(colorDanger)

	borderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorMuted).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Bold(true)

	selectedStyle = lipgloss.NewStyle().
			Foreground(colorSelected).
			Bold(true)

	runningBadgeStyle = lipgloss.NewStyle().
				Foreground(colorPanel).
				Background(colorSuccess).
				Bold(true).
				Padding(0, 1)

	stoppedBadgeStyle = lipgloss.NewStyle().
				Foreground(colorSelected).
				Background(colorMuted).
				Padding(0, 1)

	errorBadgeStyle = lipgloss.NewStyle().
			Foreground(colorSelected).
			Background(colorDanger).
			Bold(true).
			Padding(0, 1)

	connectingBadgeStyle = lipgloss.NewStyle().
				Foreground(colorPanel).
				Background(colorWarning).
				Padding(0, 1)

	labelHighlightStyle = lipgloss.NewStyle().
				Foreground(colorWarning).
				Bold(true)

	labelSelectedStyle = lipgloss.NewStyle().
				Foreground(colorWarning).
				Bold(true)

	shortcutStyle = lipgloss.NewStyle().
			Foreground(colorText)

	keyStyle = lipgloss.NewStyle().
			Foreground(colorWarning).
			Bold(true)

	lineStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	dirStyle = lipgloss.NewStyle().
			Foreground(colorAccent)

	inactiveStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	accentStyle = lipgloss.NewStyle().
			Foreground(colorAccent)

	successStyle = lipgloss.NewStyle().
			Foreground(colorSuccess)

	warningStyle = lipgloss.NewStyle().
			Foreground(colorWarning)

	dangerStyle = lipgloss.NewStyle().
			Foreground(colorDanger)

	mutedStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	sectionTitleStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true).
				Padding(0, 1)

	eyebrowStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Bold(true)

	statCardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorMuted).
			Padding(0, 2).
			MarginRight(1).
			Width(18)

	borderAccentStyle = lipgloss.NewStyle().
				Foreground(colorMuted).
				BorderForegroundBlend(colorAccent, colorAccent2)

	tableHeaderStyle = lipgloss.NewStyle().
				Foreground(colorMuted).
				Bold(true).
				Padding(0, 1)

	tableSelectedStyle = lipgloss.NewStyle().
				Foreground(colorSelected).
				Background(colorPanelHi).
				Bold(true).
				Padding(0, 1)

	tableEvenStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Padding(0, 1)

	tableOddStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Background(colorSurface).
			Padding(0, 1)

	listItemStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Padding(0, 1).
			MarginBottom(1)

	listSelectedStyle = lipgloss.NewStyle().
				Foreground(colorSelected).
				Background(colorPanelHi).
				Bold(true).
				Padding(0, 1).
				MarginBottom(1)

	commandBarStyle = lipgloss.NewStyle().
			Background(colorSurface).
			Foreground(colorText).
			Padding(0, 1)

	commandKeyStyle = lipgloss.NewStyle().
			Background(colorAccent).
			Foreground(colorPanel).
			Bold(true).
			Padding(0, 1)

	commandTextStyle = lipgloss.NewStyle().
				Foreground(colorText).
				PaddingRight(1)

	labelPillStyle = lipgloss.NewStyle().
			Foreground(colorPanel).
			Background(colorWarning).
			Bold(true).
			Padding(0, 1).
			MarginRight(1)
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
	hostCursor    int
	hostScroll    int
	typeCursor    int
	typeScroll    int
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
	trafficHist   map[int][]int64
	authQueue     *AuthPromptQueue
	promptMode    bool
	promptInput   string
	confirmQuit   bool
	authCtx       *platform.AuthContext
	cancelCtx     context.Context
	cancelFunc    context.CancelFunc

	sftpClient         *sftp.Client
	sftpLocalDir       string
	sftpRemoteDir      string
	sftpLocalFiles     []sftp.FileEntry
	sftpRemoteFiles    []sftp.FileEntry
	sftpFocus          int
	sftpCursor         [2]int
	sftpScroll         [2]int
	sftpTransferring   bool
	sftpProgress       *sftp.ProgressInfo
	sftpPreview        string
	sftpPreviewing     bool
	sftpCancel         context.CancelFunc
	sftpTunnelID       int
	sftpHostLabel      string
	sftpDone           chan struct{}
	sftpDirection      string
	sftpPrevDone       int64
	sftpRenaming       bool
	sftpRenameInput    string
	sftpSyncConfirm    bool
	sftpSyncConfirmMsg string
}

func (m *Model) setStatusMsg(msg string) {
	m.statusMsg = msg
	m.statusTicks = 3
}

func (m *Model) sampleTunnelTraffic() {
	if m.trafficHist == nil {
		m.trafficHist = make(map[int][]int64)
	}
	seen := make(map[int]struct{})
	for _, t := range m.manager.List() {
		seen[t.ID] = struct{}{}
		sample := t.UploadSpeed + t.DownloadSpeed
		history := append(m.trafficHist[t.ID], sample)
		if len(history) > 120 {
			history = history[len(history)-120:]
		}
		m.trafficHist[t.ID] = history
	}
	for id := range m.trafficHist {
		if _, ok := seen[id]; !ok {
			delete(m.trafficHist, id)
		}
	}
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
	authQueue := NewAuthPromptQueue()
	cancelCtx, cancelFunc := context.WithCancel(context.Background())
	authCtx := &platform.AuthContext{
		RequestChan: authQueue.RequestChan(),
		Cancel:      cancelCtx,
		Timeout:     30 * time.Second,
	}
	manager.SetAuthContext(authCtx)

	m := Model{
		screen:        ScreenMain,
		manager:       manager,
		hosts:         hosts,
		authQueue:     authQueue,
		authCtx:       authCtx,
		cancelCtx:     cancelCtx,
		cancelFunc:    cancelFunc,
		selectedIndex: 0,
		width:         defaultWidth,
		version:       version,
		trafficHist:   make(map[int][]int64),
	}

	return m
}

type typeItem struct {
	name string
	desc string
	t    tunnel.TunnelType
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
		return m, nil
	case sizeMsg:
		if msg.width != m.width || msg.height != m.height {
			m.width = msg.width
			m.height = msg.height
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKeyPress(msg)
	case tickMsg:
		m.sampleTunnelTraffic()
		if m.statusTicks > 0 {
			m.statusTicks--
			if m.statusTicks == 0 {
				m.statusMsg = ""
			}
		}
		if m.screen == ScreenSFTP && m.sftpDone != nil {
			if m.sftpTransferring && m.sftpProgress != nil {
				doneNow := atomic.LoadInt64(&m.sftpProgress.Done)
				m.sftpProgress.Speed = (doneNow - m.sftpPrevDone) * 2
				m.sftpPrevDone = doneNow
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

	return m, nil
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
	case ScreenSFTP:
		return m.handleSFTPKeys(msg)
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
			key := msg.Key()
			if key.Code == tea.KeySpace {
				m.promptInput += " "
			} else if key.Text != "" {
				m.promptInput += key.Text
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
		if len(m.hosts) > 0 && m.hostCursor < len(m.hosts) {
			m.selectedHost = m.hosts[m.hostCursor]
			m.screen = ScreenSelectType
			return m, nil
		}
	case "esc", "q":
		m.screen = ScreenMain
		return m, nil
	case "up", "k":
		if m.hostCursor > 0 {
			m.hostCursor--
		}
	case "down", "j":
		if m.hostCursor < len(m.hosts)-1 {
			m.hostCursor++
		}
	case "home":
		m.hostCursor = 0
	case "end":
		if len(m.hosts) > 0 {
			m.hostCursor = len(m.hosts) - 1
		}
	}
	m.hostScroll = clampScroll(m.hostCursor, m.hostScroll, selectListHeight(m.height, hostListHeight))
	return m, nil
}

func (m Model) handleTypeSelectKeys(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	items := tunnelTypeItems()
	switch keyMsg.String() {
	case "enter":
		if m.typeCursor >= 0 && m.typeCursor < len(items) {
			item := items[m.typeCursor]
			m.selectedType = item.t
			m.screen = ScreenInputPort
			m.portInput = ""
			m.inputMode = 0
			return m, nil
		}
	case "esc", "q":
		m.screen = ScreenSelectHost
		return m, nil
	case "up", "k":
		if m.typeCursor > 0 {
			m.typeCursor--
		}
	case "down", "j":
		if m.typeCursor < len(items)-1 {
			m.typeCursor++
		}
	}
	m.typeScroll = clampScroll(m.typeCursor, m.typeScroll, selectListHeight(m.height, typeListHeight))
	return m, nil
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
		if text := msg.Key().Text; text != "" {
			allowAddr := m.selectedType == tunnel.Remote || m.selectedType == tunnel.Local
			for _, r := range text {
				if validPortInputRune(r, allowAddr) {
					m.portInput += string(r)
				}
			}
		}
	}
	return m, nil
}

func (m Model) View() tea.View {
	view := tea.NewView(m.renderView())
	view.AltScreen = true
	return view
}

func (m Model) renderView() string {
	var b strings.Builder

	width := renderWidth(m.width)
	height := renderHeight(m.height)

	var title string
	if m.screen == ScreenSFTP {
		title = fmt.Sprintf("inTun SFTP  %s", m.sftpHostLabel)
	} else {
		title = "inTun  Interactive SSH Tunnel"
	}
	b.WriteString(renderTopBar(width, title, m.screenName(), m.version))
	b.WriteString("\n")

	if m.screen != ScreenSFTP {
		b.WriteString("\n")
	}

	if m.err != nil {
		b.WriteString(errorStyle.Render("Error: " + truncate(m.err.Error(), width-7)))
		b.WriteString("\n\n")
	}

	switch m.screen {
	case ScreenMain:
		b.WriteString(m.renderMainScreen())
	case ScreenSelectHost:
		b.WriteString(m.renderHostSelect())
	case ScreenSelectType:
		b.WriteString(m.renderTypeSelect())
	case ScreenInputPort:
		b.WriteString(m.renderPortInput())
	case ScreenSFTP:
		b.WriteString(m.renderSFTPScreen())
	}

	content := b.String()
	lines := strings.Count(content, "\n")
	remainingLines := height - lines - 1
	if remainingLines > 0 {
		content += strings.Repeat("\n", remainingLines)
	}

	content += m.renderShortcuts()
	if overlay := m.renderStatusOverlay(width, height); overlay != "" {
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

	if m.promptMode {
		return overlayCentered(content, m.renderPromptModal(width), width, height)
	}
	if m.confirmQuit {
		return overlayCentered(content, m.renderQuitConfirmModal(width), width, height)
	}
	return content
}

func (m Model) screenName() string {
	switch m.screen {
	case ScreenMain:
		return "TUNNELS"
	case ScreenSelectHost:
		return "HOSTS"
	case ScreenSelectType:
		return "TYPES"
	case ScreenInputPort:
		return "PORT"
	case ScreenSFTP:
		return "SFTP"
	default:
		return ""
	}
}

func renderTopBar(width int, title, mode, version string) string {
	left := titleStyle.Render(" " + title + " ")
	versionPill := lipgloss.NewStyle().
		Background(colorSurface).
		Foreground(colorMuted).
		Padding(0, 1).
		Render(version)
	modePill := lipgloss.NewStyle().
		Background(colorAccent).
		Foreground(colorPanel).
		Bold(true).
		Padding(0, 1).
		Render(mode)
	right := mutedStyle.Render(time.Now().Format("15:04"))
	centerWidth := width - lipgloss.Width(left) - lipgloss.Width(versionPill) - lipgloss.Width(modePill) - lipgloss.Width(right)
	if centerWidth < 1 {
		centerWidth = 1
	}
	rule := lipgloss.NewStyle().
		Foreground(colorMuted).
		PaddingChar('─').
		Width(centerWidth).
		Render("")
	return lipgloss.JoinHorizontal(lipgloss.Center, left, versionPill, modePill, rule, right)
}

func (m Model) renderMainScreen() string {
	var b strings.Builder
	tunnels := m.manager.List()

	if len(tunnels) == 0 {
		empty := panelStyle(renderWidth(m.width), 5, false).
			Render(lipgloss.Place(0, 3, lipgloss.Center, lipgloss.Center, mutedStyle.Render("No tunnels active. Press 'c' to create one.")))
		return empty + "\n"
	}

	lineWidth := renderWidth(m.width)
	contentWidth := lineWidth - 2
	if contentWidth < 60 {
		contentWidth = lineWidth
	}

	b.WriteString(m.renderTunnelSummary(contentWidth, len(tunnels)))
	b.WriteString("\n")

	for i, t := range tunnels {
		b.WriteString(m.renderTunnelRow(t, i, contentWidth, i == m.selectedIndex))
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

func tunnelTypeItems() []typeItem {
	return []typeItem{
		{name: "Local (-L)", desc: "Forward local port to remote server", t: tunnel.Local},
		{name: "Remote (-R)", desc: "Forward remote port to local server", t: tunnel.Remote},
		{name: "Dynamic (-D)", desc: "SOCKS proxy on local port", t: tunnel.Dynamic},
	}
}

func (m Model) renderHostSelect() string {
	width := renderWidth(m.width)
	height := selectListHeight(m.height, hostListHeight)
	m.hostScroll = clampScroll(m.hostCursor, m.hostScroll, height)

	var b strings.Builder
	b.WriteString(sectionTitleStyle.Render("Select Host"))
	b.WriteString("\n")
	if len(m.hosts) == 0 {
		b.WriteString(panelStyle(width-2, 5, false).Render(mutedStyle.Render("No hosts found in ~/.ssh/config")))
		b.WriteString("\n")
		return b.String()
	}
	end := min(len(m.hosts), m.hostScroll+height)
	for i := m.hostScroll; i < end; i++ {
		h := m.hosts[i]
		name := h.Hostname
		if name == "" {
			name = h.Name
		}
		title := name
		if len(h.Labels) > 0 {
			title += "  "
		}
		desc := fmt.Sprintf("%s@%s:%s", h.User, h.Hostname, h.Port)
		renderedTitle := selectedStyle.Render(truncate(title, width-4))
		for _, label := range h.Labels {
			renderedTitle += labelPillStyle.Render(label)
		}
		row := renderedTitle + "\n" + mutedStyle.Render(truncate(desc, width-4))
		if i == m.hostCursor {
			b.WriteString(listSelectedStyle.Width(width - 2).Render(row))
		} else {
			b.WriteString(listItemStyle.Width(width - 2).Render(row))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) renderTypeSelect() string {
	width := renderWidth(m.width)
	items := tunnelTypeItems()
	var b strings.Builder
	b.WriteString(sectionTitleStyle.Render("Select Tunnel Type"))
	b.WriteString("\n")
	for i, item := range items {
		row := item.name + "\n" + mutedStyle.Render(item.desc)
		if i == m.typeCursor {
			b.WriteString(listSelectedStyle.Width(width - 2).Render(row))
		} else {
			b.WriteString(listItemStyle.Width(width - 2).Render(row))
		}
		b.WriteString("\n")
	}
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
		Background(colorDanger).
		Foreground(colorSelected).
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
		Background(colorWarning).
		Foreground(colorPanel).
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

func (m Model) renderStatusOverlay(width, height int) string {
	if m.sftpSyncConfirm {
		return renderDialogBox(width, height, m.sftpSyncConfirmMsg, true)
	}
	if m.statusMsg == "" {
		return ""
	}
	msg := m.statusMsg
	return renderDialogBox(width, height, msg, false)
}

func renderDialogBox(width, height int, msg string, hasButtons bool) string {
	boxBg := "\x1b[48;5;236m"
	textFg := "\x1b[38;5;252m"
	labelFg := "\x1b[38;2;166;227;161m\x1b[1m"
	btnBg := "\x1b[48;5;75m\x1b[38;5;235m\x1b[1m"
	dimFg := "\x1b[38;5;102m"
	bold := "\x1b[1m"
	reset := "\x1b[0m"

	type dl struct {
		plainW int
		ansi   string
	}

	padX := 3
	padY := 1
	var lines []dl

	for i := 0; i < padY; i++ {
		lines = append(lines, dl{})
	}
	for _, ml := range strings.Split(msg, "\n") {
		trimmed := strings.TrimLeft(ml, " ")
		if strings.HasPrefix(trimmed, "FROM:") || strings.HasPrefix(trimmed, "TO:") {
			parts := strings.SplitN(trimmed, ": ", 2)
			lead := len(ml) - len(trimmed)
			label := parts[0] + ":"
			value := ""
			if len(parts) == 2 {
				value = parts[1]
			}
			pw := lead + len(label) + 1 + len(value)
			a := boxBg + strings.Repeat(" ", lead) + labelFg + label + reset + boxBg + " " + textFg + value + reset + boxBg
			lines = append(lines, dl{pw, a})
		} else {
			lines = append(lines, dl{len(ml), boxBg + textFg + bold + ml + reset + boxBg})
		}
	}
	if hasButtons {
		lines = append(lines, dl{})
		cp := "Enter Confirm"
		ep := "  Esc Cancel"
		pw := 1 + len(cp) + 1 + len(ep)
		a := btnBg + " " + cp + " " + reset + boxBg + dimFg + ep + reset + boxBg
		lines = append(lines, dl{pw, a})
	}
	for i := 0; i < padY; i++ {
		lines = append(lines, dl{})
	}

	innerW := 0
	for _, l := range lines {
		if w := l.plainW + padX*2; w > innerW {
			innerW = w
		}
	}

	styledLines := make([]string, len(lines))
	for i, l := range lines {
		if l.plainW == 0 {
			styledLines[i] = boxBg + strings.Repeat(" ", innerW) + reset
		} else {
			padR := innerW - padX - l.plainW
			if padR < padX {
				padR = padX
			}
			styledLines[i] = boxBg + strings.Repeat(" ", padX) + l.ansi + strings.Repeat(" ", padR) + reset
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
	width := renderWidth(m.width)

	type command struct {
		key   string
		label string
	}
	var items []command
	switch m.screen {
	case ScreenMain:
		items = []command{
			{"↑↓", "Navigate"},
			{"c", "Create"},
			{"f", "SFTP"},
			{"r", "Reconnect"},
			{"s", "Stop/Start"},
			{"d", "Delete"},
			{"q", "Quit"},
		}
	case ScreenSelectHost, ScreenSelectType:
		items = []command{
			{"↑↓", "Navigate"},
			{"Enter", "Select"},
			{"Esc", "Back"},
		}
	case ScreenInputPort:
		items = []command{
			{"0-9", "Input Port"},
			{"Enter", "Confirm"},
			{"Esc", "Back"},
		}
	case ScreenSFTP:
		if m.sftpPreviewing {
			items = []command{
				{"Esc", "Close Preview"},
			}
		} else if m.sftpTransferring {
			items = []command{
				{"Tab", "Switch"},
				{"↑↓", "Navigate"},
				{"●", "Transferring"},
				{"q", "Back"},
			}
		} else {
			items = []command{
				{"Tab", "Switch"},
				{"↑↓", "Navigate"},
				{"Enter", "Open"},
				{"s", "Sync"},
				{"r", "Sync Dir"},
				{"n", "Rename"},
				{"v", "Preview"},
				{"q", "Back"},
			}
		}
	}

	segments := make([]string, 0, len(items))
	for _, item := range items {
		segments = append(segments, commandSegment(item.key, item.label))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Center, segments...)
	if lipgloss.Width(body) > width {
		body = truncateANSI(body, width)
	}
	return commandBarStyle.Width(width).Render(lipgloss.PlaceHorizontal(width-2, lipgloss.Center, body))
}

func commandSegment(key, label string) string {
	return lipgloss.JoinHorizontal(lipgloss.Center,
		commandKeyStyle.Render(key),
		commandTextStyle.Render(label),
		lipgloss.NewStyle().Foreground(colorMuted).PaddingRight(1).Render("·"),
	)
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

func truncateMiddle(s string, max int) string {
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
	left := (max - 3) / 2
	right := max - 3 - left
	return string(runes[:left]) + "..." + string(runes[len(runes)-right:])
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
	border := lipgloss.NewStyle().Foreground(colorWarning)
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

func panelStyle(width, height int, focused bool) lipgloss.Style {
	style := lipgloss.NewStyle().
		Width(width).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorSurface).
		Padding(0, 1)
	if height > 0 {
		style = style.Height(height)
	}
	if focused {
		style = style.BorderForegroundBlend(colorAccent, colorAccent2)
	}
	return style
}

func statusTextStyle(status string) lipgloss.Style {
	switch status {
	case "Running":
		return successStyle
	case "Connecting":
		return warningStyle
	case "Error":
		return dangerStyle
	case "Stopped":
		return mutedStyle
	default:
		return shortcutStyle
	}
}

func (m Model) renderTunnelSummary(width int, total int) string {
	var running, connecting, stopped, errored int
	var tx, rx int64
	for _, t := range m.manager.List() {
		switch t.GetStatus() {
		case tunnel.StatusRunning:
			running++
		case tunnel.StatusConnecting:
			connecting++
		case tunnel.StatusStopped:
			stopped++
		case tunnel.StatusError:
			errored++
		}
		tx += t.UploadBytes
		rx += t.DownloadBytes
	}
	cards := []string{
		metricPill("ALL", fmt.Sprintf("%d", total), accentStyle),
		metricPill("RUN", fmt.Sprintf("%d", running), successStyle),
		metricPill("CONN", fmt.Sprintf("%d", connecting), warningStyle),
		metricPill("ERR", fmt.Sprintf("%d", errored), dangerStyle),
		metricPill("STOP", fmt.Sprintf("%d", stopped), mutedStyle),
		metricPill("FLOW", formatTotal(tx, "TX")+"  "+formatTotal(rx, "RX"), accentStyle),
	}
	return lipgloss.NewStyle().
		Width(width).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(colorMuted).
		PaddingBottom(1).
		Render(lipgloss.JoinHorizontal(lipgloss.Center, cards...))
}

func metricPill(label, value string, valueStyle lipgloss.Style) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorMuted).
		Padding(0, 1).
		MarginRight(1).
		Render(eyebrowStyle.Render(label) + " " + valueStyle.Bold(true).Render(value))
}

func (m Model) renderTunnelRow(t *tunnel.Tunnel, idx, width int, focused bool) string {
	status := tunnelStatusLabel(t)
	borderColor := colorSurface
	if focused {
		borderColor = colorAccent
	}
	rowTextStyle := lipgloss.NewStyle()
	rowMutedStyle := mutedStyle
	rowAccentStyle := accentStyle
	rowSelectedStyle := selectedStyle
	local := formatTunnelAddr(t.LocalPort)
	remote := "-"
	if t.Type == tunnel.Dynamic {
		remote = "SOCKS5"
	} else if t.RemotePort != "" {
		remote = formatTunnelAddr(t.RemotePort)
	}
	latency := "-"
	if t.Latency > 0 {
		latency = fmt.Sprintf("%dms", t.Latency.Milliseconds())
	}
	statusBadge := lipgloss.NewStyle().
		Foreground(colorPanel).
		Background(statusColor(status)).
		Bold(true).
		Padding(0, 1).
		Render(status)
	textW := width - 4
	nameMax := textW - lipgloss.Width(statusBadge) - 18
	if nameMax < 14 {
		nameMax = 14
	}
	left := rowMutedStyle.Render(fmt.Sprintf("#%d", t.ID)) + rowTextStyle.Render(" ") + rowSelectedStyle.Render(truncate(t.Name, nameMax))
	right := statusBadge
	first := left + lipgloss.PlaceHorizontal(textW-lipgloss.Width(left), lipgloss.Right, right)
	uploadSpeed := formatSpeed(t.UploadSpeed, "↑")
	downloadSpeed := formatSpeed(t.DownloadSpeed, "↓")
	uploadTotal := formatTotal(t.UploadBytes, "TX")
	downloadTotal := formatTotal(t.DownloadBytes, "RX")
	metrics := fmt.Sprintf("%s %s  %s %s", uploadSpeed, downloadSpeed, uploadTotal, downloadTotal)
	secondRight := fmt.Sprintf("%s   %s", latency, metrics)
	rightWidth := lipgloss.Width(secondRight)
	maxRightWidth := max(12, textW-18)
	if rightWidth > maxRightWidth {
		secondRight = truncate(secondRight, maxRightWidth)
		rightWidth = lipgloss.Width(secondRight)
	}
	leftWidth := textW - rightWidth - 1
	if leftWidth < 12 {
		leftWidth = 12
	}
	route := fmt.Sprintf("%s → %s", local, remote)
	typeLabel := tunnelTypeLabel(t.Type)
	secondLeft := rowAccentStyle.Render(typeLabel) + rowTextStyle.Render("  ") + rowTextStyle.Render(truncate(route, leftWidth-lipgloss.Width(typeLabel)-2))
	rightRendered := rowMutedStyle.Width(rightWidth).Render(secondRight)
	second := fitLine(secondLeft, leftWidth) + " " + rightRendered
	flow := renderTrafficFlow(m.trafficHist[t.ID], textW)
	lines := []string{first, second, flow}
	if t.Status == tunnel.StatusError && t.Error != "" {
		lines = append(lines, renderTunnelErrorHint(t, textW-2)...)
	}
	rowStyle := lipgloss.NewStyle().
		Width(width).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		MarginBottom(1)
	if focused {
		rowStyle = rowStyle.
			BorderForegroundBlend(colorAccent, colorAccent2)
	}
	_ = idx
	return rowStyle.Render(strings.Join(lines, "\n"))
}

func renderTrafficFlow(history []int64, width int) string {
	if width < 20 {
		width = 20
	}
	graphWidth := width
	if graphWidth < 8 {
		graphWidth = 8
	}
	if len(history) == 0 {
		return mutedStyle.Render(strings.Repeat("·", graphWidth))
	}
	if len(history) > graphWidth {
		history = history[len(history)-graphWidth:]
	}
	pad := graphWidth - len(history)
	scale := int64(16 * 1024)
	for _, sample := range history {
		if sample > scale {
			scale = sample
		}
	}
	scale = max(scale, 64*1024)

	blocks := []rune("▁▂▃▄▅▆▇█")
	var b strings.Builder
	if pad > 0 {
		b.WriteString(mutedStyle.Render(strings.Repeat("·", pad)))
	}
	for _, sample := range history {
		if sample <= 0 {
			b.WriteString(mutedStyle.Render("·"))
			continue
		}
		level := int((sample * int64(len(blocks)-1)) / scale)
		if level < 0 {
			level = 0
		}
		if level >= len(blocks) {
			level = len(blocks) - 1
		}
		style := mutedStyle
		if level >= 5 {
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
		} else if level >= 3 {
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#56616F"))
		}
		b.WriteString(style.Render(string(blocks[level])))
	}
	return b.String()
}

func tunnelStatusLabel(t *tunnel.Tunnel) string {
	switch t.Status {
	case tunnel.StatusRunning:
		return "Running"
	case tunnel.StatusError:
		return "Error"
	case tunnel.StatusConnecting:
		return "Connecting"
	case tunnel.StatusStopped:
		return "Stopped"
	default:
		return "-"
	}
}

func tunnelTypeLabel(t tunnel.TunnelType) string {
	return strings.ToUpper(t.String())
}

func statusColor(status string) color.Color {
	switch status {
	case "Running":
		return colorSuccess
	case "Connecting":
		return colorWarning
	case "Error":
		return colorDanger
	case "Stopped":
		return colorMuted
	default:
		return colorAccent
	}
}

func renderTunnelErrorHint(t *tunnel.Tunnel, width int) []string {
	errMsg := t.Error
	switch {
	case strings.Contains(errMsg, "SSH_AUTH_FAILED"):
		return []string{
			dangerStyle.Render("Authentication failed. Check SSH key:"),
			mutedStyle.Render("Ensure valid key in ~/.ssh/id_rsa or ~/.ssh/id_ed25519, or specify IdentityFile in ~/.ssh/config"),
		}
	case strings.Contains(errMsg, "SSH_CONNECTION_FAILED"):
		return []string{dangerStyle.Render("Connection failed:"), mutedStyle.Render(truncate(errMsg, width))}
	case strings.Contains(errMsg, "HOST_KEY_NOT_CACHED"):
		return []string{
			dangerStyle.Render("Host key not cached. Run manually:"),
			selectedStyle.Render(fmt.Sprintf("ssh %s@%s -p %s", t.SSHConfig.User, t.SSHConfig.Host, t.SSHConfig.Port)),
		}
	case strings.Contains(errMsg, "SSH_CONNECTION_LOST"):
		return []string{dangerStyle.Render("SSH connection lost - press 'r' to reconnect")}
	default:
		return []string{dangerStyle.Render("Error: " + truncate(errMsg, width))}
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

func selectListHeight(height, maxHeight int) int {
	listHeight := height - 8
	if listHeight > maxHeight {
		listHeight = maxHeight
	}
	if listHeight < 5 {
		listHeight = 5
	}
	return listHeight
}

func clampScroll(cursor, scroll, height int) int {
	if cursor < scroll {
		return cursor
	}
	if cursor >= scroll+height {
		return cursor - height + 1
	}
	if scroll < 0 {
		return 0
	}
	return scroll
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
	h := m.height - sftpChromeHeight()
	h -= m.sftpDrawerHeight()
	if h < 5 {
		h = 5
	}
	return h
}

func sftpChromeHeight() int {
	return 13
}

func (m Model) sftpDrawerHeight() int {
	height := 0
	if m.sftpRenaming {
		height += 3
	}
	if m.sftpTransferring {
		height += 4
	}
	if m.sftpPreviewing {
		height += m.sftpPreviewHeight() + 3
	}
	return height
}

func (m Model) sftpPreviewHeight() int {
	height := m.height / 4
	if height < 4 {
		height = 4
	}
	if height > 8 {
		height = 8
	}
	return height
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
				atomic.StoreInt64(&m.sftpProgress.Done, n)
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
			atomic.StoreInt64(&m.sftpProgress.Done, n)
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
	m.sftpSyncConfirmMsg = fmt.Sprintf("FROM: %s\n  TO: %s", src, dst)
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
				atomic.StoreInt64(&m.sftpProgress.Done, done)
				atomic.StoreInt64(&m.sftpProgress.Total, total)
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
	localPath := filepath.Join(m.sftpLocalDir, entry.Name)
	m.sftpTransferring = true
	m.sftpDirection = "⇣"
	m.sftpProgress = &sftp.ProgressInfo{File: entry.Name, Active: true}
	m.sftpPrevDone = 0
	done := make(chan struct{})
	m.sftpDone = done
	client := m.sftpClient
	go func() {
		client.DownloadDir(remotePath, localPath, func(done, total int64, file string) {
			atomic.StoreInt64(&m.sftpProgress.Done, done)
			atomic.StoreInt64(&m.sftpProgress.Total, total)
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

func (m Model) renderSFTPTabBar(panelWidth int) string {
	activeStyle := lipgloss.NewStyle().
		Background(colorAccent).
		Foreground(colorPanel).
		Bold(true).
		Padding(0, 3)
	inactiveStyle := lipgloss.NewStyle().
		Foreground(colorMuted).
		Background(colorSurface).
		Padding(0, 3)

	var left, right string
	if m.sftpFocus == 0 {
		left = activeStyle.Render("LOCAL")
		right = inactiveStyle.Render("REMOTE")
	} else {
		left = inactiveStyle.Render("LOCAL")
		right = activeStyle.Render("REMOTE")
	}

	tabs := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
	return lipgloss.PlaceHorizontal(panelWidth*2+1, lipgloss.Center, tabs)
}

func (m Model) renderSFTPScreen() string {
	width := renderWidth(m.width)
	panelWidth := (width - 3) / 2
	if panelWidth < 20 {
		panelWidth = 20
	}

	var b strings.Builder
	panelHeight := m.sftpVisibleHeight() + 2
	b.WriteString(m.renderSFTPTabBar(panelWidth))
	b.WriteString("\n")
	localPanel := m.renderSFTPPanel("LOCAL", m.sftpLocalDir, m.sftpLocalFiles, 0, panelWidth, panelHeight, m.sftpFocus == 0)
	remotePanel := m.renderSFTPPanel("REMOTE", m.sftpRemoteDir, m.sftpRemoteFiles, 1, panelWidth, panelHeight, m.sftpFocus == 1)
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, localPanel, " ", remotePanel))
	b.WriteString("\n")

	if m.sftpRenaming {
		input := m.sftpRenameInput + "_"
		hint := "Rename: " + input
		confirmHint := "  [Enter]Confirm [Esc]Cancel"
		rendered := warningStyle.Render(hint) + mutedStyle.Render(confirmHint)
		b.WriteString(panelStyle(width-2, 0, true).Render(rendered))
		b.WriteString("\n")
	}

	if m.sftpTransferring {
		b.WriteString(panelStyle(width-2, 0, false).Render(m.renderSFTPProgress(width - 6)))
		b.WriteString("\n")
	}

	if m.sftpPreviewing {
		b.WriteString(panelStyle(width-2, m.sftpPreviewHeight()+2, true).Render(m.renderSFTPPreview(width-6, m.sftpPreviewHeight())))
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderSFTPPanel(label, dir string, files []sftp.FileEntry, panelIdx, width, height int, focused bool) string {
	var b strings.Builder

	count := fmt.Sprintf("%d items", len(files))
	title := eyebrowStyle.Render(label) + " " + mutedStyle.Render(count)
	if focused {
		title = accentStyle.Bold(true).Render(label) + " " + mutedStyle.Render(count)
	}
	title = title + lipgloss.PlaceHorizontal(width-lipgloss.Width(title)-2, lipgloss.Right, mutedStyle.Render(truncateMiddle(dir, max(10, width/2))))
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(lineStyle.Render(strings.Repeat("─", max(1, width-4))))
	b.WriteString("\n")

	visibleHeight := height - 4
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
		b.WriteString(m.renderSFTPEntryLine(files, i, cursor, width, focused))
		b.WriteString("\n")
		renderIdx++
	}
	for renderIdx < visibleHeight {
		b.WriteString(mutedStyle.Render(strings.Repeat(" ", max(1, width-4))))
		b.WriteString("\n")
		renderIdx++
	}

	return panelStyle(width, height, focused).Render(b.String())
}

func (m Model) renderSFTPEntryLine(files []sftp.FileEntry, i, cursor, width int, focused bool) string {
	var name, sizeStr string
	var isDir bool

	if i == 0 {
		name = ".."
		isDir = true
	} else {
		idx := i - 1
		if idx >= len(files) {
			return ""
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
		prefix = "❯ "
	}
	marker := " "
	if isDir {
		marker = "/"
	}

	var line string
	if sizeStr != "" {
		nameWidth := width - 8 - len(sizeStr)
		if nameWidth < 5 {
			nameWidth = 5
		}
		truncated := truncate(displayName, nameWidth)
		padLen := width - 8 - lipgloss.Width(truncated) - len(sizeStr)
		if padLen < 1 {
			padLen = 1
		}
		line = prefix + marker + " " + truncated + strings.Repeat(" ", padLen) + sizeStr
	} else {
		line = prefix + marker + " " + truncate(displayName, width-8)
	}

	linePad := width - 4 - lipgloss.Width(line)
	if linePad > 0 {
		line += strings.Repeat(" ", linePad)
	}

	if i == cursor && focused {
		return lipgloss.NewStyle().Foreground(colorSelected).Background(colorPanelHi).Bold(true).Render(line)
	}
	if i == cursor {
		if isDir {
			return dirStyle.Render(line)
		} else {
			return inactiveStyle.Render(line)
		}
	}
	if isDir {
		return dirStyle.Render(line)
	}
	return shortcutStyle.Render(line)
}

func (m Model) renderSFTPPreview(width, maxLines int) string {
	var b strings.Builder
	b.WriteString(accentStyle.Bold(true).Render("Preview"))
	b.WriteString("\n")

	lines := strings.Split(m.sftpPreview, "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}

	previewStyle := lipgloss.NewStyle().Foreground(colorText).Width(width)
	for _, line := range lines {
		displayLine := truncate(line, width)
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
	doneNow := atomic.LoadInt64(&p.Done)
	totalNow := atomic.LoadInt64(&p.Total)
	if totalNow > 0 {
		pct = float64(doneNow) / float64(totalNow)
		if pct > 1 {
			pct = 1
		}
	}

	var speedStr string
	if p.Speed > 0 {
		speedStr = " " + formatTransferSpeed(p.Speed)
	}

	barWidth := width
	if barWidth < 20 {
		barWidth = 20
	}

	infoLine := warningStyle.Render(fmt.Sprintf("%s %s %.0f%%%s", m.sftpDirection, p.File, pct*100, speedStr))

	return infoLine + "\n" + renderProgressBar(pct, barWidth)
}

func renderProgressBar(pct float64, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	if width < 1 {
		return ""
	}
	filled := int(float64(width) * pct)
	if pct > 0 && filled == 0 {
		filled = 1
	}
	if filled > width {
		filled = width
	}
	bar := successStyle.Render(strings.Repeat("━", filled)) + mutedStyle.Render(strings.Repeat("━", width-filled))
	return bar
}
