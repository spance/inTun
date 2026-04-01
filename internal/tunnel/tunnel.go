package tunnel

import (
	"fmt"
	"sync"
	"time"

	"github.com/spance/intun/internal/platform"
)

type TunnelType = platform.TunnelType

const (
	Local   TunnelType = platform.Local
	Remote  TunnelType = platform.Remote
	Dynamic TunnelType = platform.Dynamic
)

type Status int

const (
	StatusStopped Status = iota
	StatusRunning
	StatusConnecting
	StatusError
)

func (s Status) String() string {
	switch s {
	case StatusStopped:
		return "Stopped"
	case StatusRunning:
		return "Running"
	case StatusConnecting:
		return "Connecting"
	case StatusError:
		return "Error"
	default:
		return "Unknown"
	}
}

type Tunnel struct {
	ID            int
	Name          string
	SSHConfig     *platform.SSHConfig
	Type          TunnelType
	LocalPort     string
	RemotePort    string
	Status        Status
	Conn          platform.Connection
	Error         string
	CreatedAt     time.Time
	UploadBytes   int64
	DownloadBytes int64
	UploadSpeed   int64
	DownloadSpeed int64
	Latency       time.Duration
	PingFailCount int
	mu            sync.RWMutex
}

type Manager struct {
	Tunnels  []*Tunnel
	nextID   int
	mu       sync.RWMutex
	executor platform.Executor
	authCtx  *platform.AuthContext
}

func NewManager(authCtx *platform.AuthContext) *Manager {
	return &Manager{
		Tunnels:  make([]*Tunnel, 0),
		nextID:   1,
		executor: platform.NewExecutor(),
		authCtx:  authCtx,
	}
}

func (m *Manager) SetAuthContext(ctx *platform.AuthContext) {
	m.mu.Lock()
	m.authCtx = ctx
	m.mu.Unlock()
}

func (m *Manager) SetExecutor(exec platform.Executor) {
	m.mu.Lock()
	m.executor = exec
	m.mu.Unlock()
}

func (m *Manager) Create(name string, cfg *platform.SSHConfig, tunnelType TunnelType, localPort, remotePort string) (*Tunnel, error) {
	m.mu.Lock()

	t := &Tunnel{
		ID:         m.nextID,
		Name:       name,
		SSHConfig:  cfg,
		Type:       tunnelType,
		LocalPort:  localPort,
		RemotePort: remotePort,
		Status:     StatusConnecting,
		CreatedAt:  time.Now(),
	}
	m.nextID++
	m.Tunnels = append(m.Tunnels, t)
	m.mu.Unlock()

	err := m.startTunnel(t)
	if err != nil {
		t.Status = StatusError
		t.Error = err.Error()
		return t, err
	}

	return t, nil
}

func (m *Manager) startTunnel(t *Tunnel) error {
	conn, err := m.executor.Connect(m.authCtx, t.SSHConfig, t.Type, t.LocalPort, t.RemotePort)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	t.Conn = conn
	if !conn.IsRunning() {
		t.Status = StatusError
		t.Error = conn.Error()
		return fmt.Errorf("connection failed immediately: %s", conn.Error())
	}
	t.Status = StatusRunning
	t.Error = ""
	return nil
}

func (m *Manager) Stop(id int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, t := range m.Tunnels {
		if t.ID == id {
			if t.Conn != nil {
				t.Conn.Stop()
			}
			t.Status = StatusStopped
			return nil
		}
	}
	return fmt.Errorf("tunnel %d not found", id)
}

func (m *Manager) Restart(id int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, t := range m.Tunnels {
		if t.ID == id {
			if t.Conn != nil {
				t.Conn.Stop()
			}
			time.Sleep(1 * time.Second)
			t.Status = StatusConnecting
			err := m.startTunnel(t)
			if err != nil {
				t.Status = StatusError
				t.Error = err.Error()
			}
			return err
		}
	}
	return fmt.Errorf("tunnel %d not found", id)
}

func (m *Manager) Delete(id int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, t := range m.Tunnels {
		if t.ID == id {
			if t.Conn != nil {
				t.Conn.Stop()
			}
			m.Tunnels = append(m.Tunnels[:i], m.Tunnels[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("tunnel %d not found", id)
}

func (m *Manager) Get(id int) *Tunnel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, t := range m.Tunnels {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func (m *Manager) List() []*Tunnel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.Tunnels
}

func (t *Tunnel) UpdateStats(uploadBytes, downloadBytes, uploadSpeed, downloadSpeed int64, latency time.Duration, shouldPing bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.UploadBytes = uploadBytes
	t.DownloadBytes = downloadBytes
	t.UploadSpeed = uploadSpeed
	t.DownloadSpeed = downloadSpeed

	if shouldPing {
		if latency == 0 && t.Status == StatusRunning {
			t.PingFailCount++
			if t.PingFailCount >= 3 {
				t.Status = StatusError
				t.Error = "connection lost (ping timeout)"
			}
		} else if latency > 0 {
			t.PingFailCount = 0
			t.Latency = latency
		}
	}
}

func (t *Tunnel) CheckStatus() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.Conn != nil && t.Status == StatusRunning {
		if !t.Conn.IsRunning() {
			t.Status = StatusError
			t.Error = t.Conn.Error()
		}
	}
}

func (t *Tunnel) GetStatus() Status {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Status
}

func (t *Tunnel) GetError() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Error
}
