package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spance/intun/internal/config"
	"github.com/spance/intun/internal/platform"
	"github.com/spance/intun/internal/tunnel"
)

type AuthRequest = platform.AuthRequest
type AuthResponse = platform.AuthResponse

const (
	minTermWidth   = 32
	defaultWidth   = 120
	defaultHeight  = 30
	hostListHeight = 30
	typeListHeight = 12
)

type Screen int

const (
	ScreenMain Screen = iota
	ScreenSelectHost
	ScreenInputHost
	ScreenSelectType
	ScreenInputPort
	ScreenSFTP
)

type Model struct {
	manager *tunnel.Manager
	appState
	hostSelectionState
	tunnelCreationState
	tunnelListState
	noticeState
	authState
	sftpState
	runtimeState
	componentState
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
		RequestChan:    authQueue.RequestChan(),
		Cancel:         cancelCtx,
		CancelRequests: authQueue.CancelAll,
		Timeout:        30 * time.Second,
	}
	manager.SetAuthContext(authCtx)

	return Model{
		manager: manager,
		appState: appState{
			screen:  ScreenMain,
			width:   defaultWidth,
			version: version,
		},
		hostSelectionState: hostSelectionState{
			hosts:      hosts,
			hostFilter: newTextInput("alias, host, user or label"),
		},
		tunnelCreationState: tunnelCreationState{
			manualHostInput: newTextInput("user@host:port"),
		},
		tunnelListState: tunnelListState{
			trafficHist: make(map[int][]int64),
		},
		authState: authState{authQueue: authQueue},
		runtimeState: runtimeState{
			authCtx:    authCtx,
			cancelCtx:  cancelCtx,
			cancelFunc: cancelFunc,
		},
		componentState: newComponentState(),
	}
}

type typeItem struct {
	name string
	desc string
	t    tunnel.TunnelType
	p    tunnel.NetworkProtocol
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
			return tickMsg{}
		}),
		m.pollAuthRequests(),
		m.spinner.Tick,
	)
}

func (m Model) Close() error {
	if m.sftpOperationCancel != nil {
		m.sftpOperationCancel()
	}
	if m.sftpCancel != nil {
		m.sftpCancel()
	}
	if m.cancelFunc != nil {
		m.cancelFunc()
	}
	if m.sftpClient != nil {
		return m.sftpClient.Close()
	}
	return nil
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
