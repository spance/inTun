package tui

import (
	"context"
	"fmt"
	"image/color"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
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
			Border(uiBorder()).
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
			Border(uiBorder()).
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

	sftpClient              *sftp.Client
	sftpLocalDir            string
	sftpRemoteDir           string
	sftpLocalFiles          []sftp.FileEntry
	sftpRemoteFiles         []sftp.FileEntry
	sftpFocus               int
	sftpCursor              [2]int
	sftpScroll              [2]int
	sftpTransferring        bool
	sftpProgress            *sftp.ProgressInfo
	sftpPreview             string
	sftpPreviewing          bool
	sftpCancel              context.CancelFunc
	sftpTransferID          int
	sftpTunnelID            int
	sftpHostLabel           string
	sftpDone                chan sftpTransferResult
	sftpDirection           string
	sftpPrevDone            int64
	sftpRenaming            bool
	sftpRenameInput         string
	sftpSyncConfirm         bool
	sftpSyncConfirmMsg      string
	sftpOverwriteConfirm    bool
	sftpOverwriteConfirmMsg string
	sftpPendingSync         sftpPendingSync
}

type sftpTransferResult struct {
	id        int
	err       error
	source    string
	target    string
	direction string
	report    sftp.TransferReport
}

type sftpPendingSync struct {
	focus  int
	source string
	target string
	name   string
	size   int64
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
		if m.screen == ScreenSFTP {
			m = m.normalizeSFTPScroll()
		}
		return m, nil
	case sizeMsg:
		if msg.width != m.width || msg.height != m.height {
			m.width = msg.width
			m.height = msg.height
			if m.screen == ScreenSFTP {
				m = m.normalizeSFTPScroll()
			}
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
		if m.sftpDone != nil {
			if m.sftpTransferring && m.sftpProgress != nil {
				snapshot := m.sftpProgress.Snapshot()
				m.sftpProgress.SetSpeed((snapshot.Done - m.sftpPrevDone) * 2)
				m.sftpPrevDone = snapshot.Done
			}
			select {
			case result := <-m.sftpDone:
				if result.id != m.sftpTransferID {
					m.sftpDone = nil
				} else {
					m.sftpTransferring = false
					m.sftpCancel = nil
					if m.sftpProgress != nil {
						m.sftpProgress.SetActive(false)
					}
					if m.screen == ScreenSFTP {
						if result.err != nil {
							m.setStatusMsg(formatSFTPTransferError(result))
						} else {
							m.setStatusMsg(formatSFTPTransferSuccess(result))
						}
					}
					if result.err == nil && m.screen == ScreenSFTP && m.sftpClient != nil {
						var refreshErr error
						m, refreshErr = m.refreshSFTPFiles()
						if refreshErr != nil {
							m.setStatusMsg(fmt.Sprintf("Transfer finished, but refresh failed: %v", refreshErr))
						}
					}
					m.sftpDone = nil
				}
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
				remoteFiles, rerr := m.sftpClient.ReadRemoteDir(m.sftpContext(), m.sftpRemoteDir)
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
				m = m.normalizeSFTPScroll()
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
	case "pgup":
		m.hostCursor -= hostSelectVisibleItems(m.height)
		if m.hostCursor < 0 {
			m.hostCursor = 0
		}
	case "pgdown":
		m.hostCursor += hostSelectVisibleItems(m.height)
		if m.hostCursor >= len(m.hosts) {
			m.hostCursor = len(m.hosts) - 1
		}
	case "home":
		m.hostCursor = 0
	case "end":
		if len(m.hosts) > 0 {
			m.hostCursor = len(m.hosts) - 1
		}
	}
	m.hostScroll = clampScroll(m.hostCursor, m.hostScroll, hostSelectVisibleItems(m.height))
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
	case "pgup":
		m.typeCursor -= selectListVisibleItems(m.height, typeListHeight)
		if m.typeCursor < 0 {
			m.typeCursor = 0
		}
	case "pgdown":
		m.typeCursor += selectListVisibleItems(m.height, typeListHeight)
		if m.typeCursor >= len(items) {
			m.typeCursor = len(items) - 1
		}
	}
	m.typeScroll = clampScroll(m.typeCursor, m.typeScroll, selectListVisibleItems(m.height, typeListHeight))
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
	if overlay := m.renderStatusOverlay(width); overlay.content != "" {
		content = overlayCentered(content, overlay, width, height)
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
	visibleItems := hostSelectVisibleItems(m.height)
	m.hostScroll = clampScroll(m.hostCursor, m.hostScroll, visibleItems)

	var b strings.Builder
	title := "Select Host"
	if len(m.hosts) > 0 {
		end := min(len(m.hosts), m.hostScroll+visibleItems)
		title = fmt.Sprintf("Select Host  %d-%d/%d", m.hostScroll+1, end, len(m.hosts))
	}
	b.WriteString(sectionTitleStyle.Render(title))
	b.WriteString("\n")
	if len(m.hosts) == 0 {
		b.WriteString(panelStyle(width-2, 5, false).Render(mutedStyle.Render("No hosts found in ~/.ssh/config")))
		b.WriteString("\n")
		return b.String()
	}
	end := min(len(m.hosts), m.hostScroll+visibleItems)
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
	visibleItems := selectListVisibleItems(m.height, typeListHeight)
	m.typeScroll = clampScroll(m.typeCursor, m.typeScroll, visibleItems)
	var b strings.Builder
	b.WriteString(sectionTitleStyle.Render("Select Tunnel Type"))
	b.WriteString("\n")
	end := min(len(items), m.typeScroll+visibleItems)
	for i := m.typeScroll; i < end; i++ {
		item := items[i]
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

	if current.Type == platform.AuthRequestHostKey {
		body := []string{"Unknown host key"}
		if pending := m.authQueue.PendingCount(); pending > 0 {
			body = append(body, fmt.Sprintf("%d more auth request(s) queued", pending))
		}
		return renderModalSpec(width, ModalSpec{
			Title:    "Auth Required",
			Severity: ModalDanger,
			Body:     body,
			Fields: []ModalField{
				{Label: "Host", Value: current.Host},
				{Label: "Fingerprint", Value: current.Fingerprint},
			},
			Actions: []ModalAction{
				{Key: "A", Label: "Accept"},
				{Key: "R", Label: "Reject"},
			},
			Width: min(64, width-8),
		})
	}

	attempt := current.RetryCount + 1
	mask := strings.Repeat("*", len([]rune(m.promptInput)))
	body := []string{fmt.Sprintf("Password required  attempt %d/3", attempt)}
	if pending := m.authQueue.PendingCount(); pending > 0 {
		body = append(body, fmt.Sprintf("%d more auth request(s) queued", pending))
	}
	return renderModalSpec(width, ModalSpec{
		Title:    "Auth Required",
		Severity: ModalDanger,
		Body:     body,
		Fields: []ModalField{
			{Label: "Host", Value: current.Host},
			{Label: "Password", Value: "[" + mask + "]"},
		},
		Actions: []ModalAction{
			{Key: "Enter", Label: "Submit"},
			{Key: "Esc", Label: "Cancel"},
		},
		Width: min(64, width-8),
	})
}

func (m Model) renderQuitConfirmModal(width int) ModalView {
	liveCount := 0
	for _, t := range m.manager.List() {
		status := t.GetStatus()
		if status == tunnel.StatusRunning || status == tunnel.StatusConnecting {
			liveCount++
		}
	}

	return renderModalSpec(width, ModalSpec{
		Title:    "Confirm Exit",
		Severity: ModalWarning,
		Body: []string{
			"Active tunnels are still running",
			fmt.Sprintf("%d live tunnel(s) will be closed when inTun exits.", liveCount),
		},
		Actions: []ModalAction{
			{Key: "Enter/Y/Q", Label: "Exit"},
			{Key: "Esc/N", Label: "Cancel"},
		},
		Width: 56,
	})
}

func (m Model) renderStatusOverlay(width int) ModalView {
	if m.sftpOverwriteConfirm {
		body, fields := modalMessageParts(m.sftpOverwriteConfirmMsg)
		return renderModalSpec(width, ModalSpec{
			Title:    "Confirm Overwrite",
			Severity: ModalDanger,
			Body:     body,
			Fields:   fields,
			Actions: []ModalAction{
				{Key: "Enter", Label: "Overwrite"},
				{Key: "Esc", Label: "Cancel"},
			},
		})
	}
	if m.sftpSyncConfirm {
		body, fields := modalMessageParts(m.sftpSyncConfirmMsg)
		return renderModalSpec(width, ModalSpec{
			Title:    "Confirm Directory Sync",
			Severity: ModalWarning,
			Body:     body,
			Fields:   fields,
			Actions: []ModalAction{
				{Key: "Enter", Label: "Confirm"},
				{Key: "Esc", Label: "Cancel"},
			},
		})
	}
	if m.statusMsg == "" {
		return ModalView{}
	}
	body, fields := modalMessageParts(m.statusMsg)
	return renderModalSpec(width, ModalSpec{
		Title:  "Notice",
		Body:   body,
		Fields: fields,
	})
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

func uiBorder() lipgloss.Border {
	return lipgloss.RoundedBorder()
}

func panelStyle(width, height int, focused bool) lipgloss.Style {
	style := lipgloss.NewStyle().
		Width(width).
		Border(uiBorder()).
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
		Border(uiBorder(), false, false, true, false).
		BorderForeground(colorMuted).
		PaddingBottom(1).
		Render(lipgloss.JoinHorizontal(lipgloss.Center, cards...))
}

func metricPill(label, value string, valueStyle lipgloss.Style) string {
	return lipgloss.NewStyle().
		Border(uiBorder()).
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
		Border(uiBorder()).
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

func hostSelectVisibleItems(height int) int {
	return selectListVisibleItems(height, hostListHeight)
}

func selectListVisibleItems(height, maxItems int) int {
	rowHeight := 3
	items := selectListHeight(height, maxItems*rowHeight) / rowHeight
	if items < 1 {
		return 1
	}
	if items > maxItems {
		return maxItems
	}
	return items
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
