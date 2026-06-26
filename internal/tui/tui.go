package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spance/intun/internal/config"
	"github.com/spance/intun/internal/platform"
	"github.com/spance/intun/internal/sftp"
	"github.com/spance/intun/internal/tunnel"
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
	statusConfirm bool
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
	m.statusConfirm = false
}

func (m *Model) setStatusConfirm(msg string) {
	m.statusMsg = msg
	m.statusTicks = 0
	m.statusConfirm = true
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

	return Model{
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
