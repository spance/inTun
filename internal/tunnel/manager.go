package tunnel

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spance/intun/internal/platform"
)

type Manager struct {
	tunnels  []*Tunnel
	nextID   int
	mu       sync.RWMutex
	executor platform.Executor
	authCtx  *platform.AuthContext
}

func NewManager(authCtx *platform.AuthContext) *Manager {
	return &Manager{
		tunnels:  make([]*Tunnel, 0),
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
	return m.CreateWithProtocol(name, cfg, tunnelType, TCP, localPort, remotePort)
}

func (m *Manager) CreateWithProtocol(name string, cfg *platform.SSHConfig, tunnelType TunnelType, protocol NetworkProtocol, localPort, remotePort string) (*Tunnel, error) {
	m.mu.Lock()
	t := &Tunnel{
		id:        m.nextID,
		name:      name,
		forward:   platform.ForwardSpec{Type: tunnelType, Protocol: protocol, LocalAddr: localPort, RemoteAddr: remotePort},
		status:    StatusConnecting,
		createdAt: time.Now(),
	}
	if cfg != nil {
		t.sshConfig = *cfg
		t.hasSSHConfig = true
	}
	m.nextID++
	m.tunnels = append(m.tunnels, t)
	m.mu.Unlock()

	if err := m.startTunnel(t); err != nil {
		return t, err
	}
	return t, nil
}

func (m *Manager) startTunnel(t *Tunnel) error {
	m.mu.RLock()
	executor := m.executor
	authCtx := m.authCtx
	m.mu.RUnlock()
	if executor == nil {
		err := errors.New("no tunnel executor configured")
		t.setFailure(0, platform.NewFailure(platform.FailureSSHConnection, "start", err))
		return err
	}

	baseCtx := context.Background()
	authCopy := platform.AuthContext{}
	if authCtx != nil {
		authCopy = *authCtx
		if authCtx.Cancel != nil {
			baseCtx = authCtx.Cancel
		}
	}
	runCtx, cancel := context.WithCancel(baseCtx)
	authCopy.Cancel = runCtx
	authCopy.TunnelID = t.ID()

	generation, previous, err := t.beginStart(cancel)
	if err != nil {
		cancel()
		return err
	}
	m.stopDetachedRuntime(t.ID(), t, previous)

	cfg := t.SSHConfig()
	conn, err := executor.Connect(&authCopy, cfg, t.ForwardSpec())
	if err != nil {
		cancel()
		failure := platform.FailureFromError(fmt.Errorf("failed to connect: %w", err))
		t.setFailure(generation, failure)
		return err
	}
	if conn == nil {
		cancel()
		err = errors.New("executor returned a nil connection")
		t.setFailure(generation, platform.NewFailure(platform.FailureSSHConnection, "connect", err))
		return err
	}
	if !t.installConnection(generation, conn) {
		cancel()
		_ = conn.Stop()
		return context.Canceled
	}
	return nil
}

func (m *Manager) Stop(id int) error {
	t := m.find(id)
	if t == nil {
		return fmt.Errorf("tunnel %d not found", id)
	}
	runtime := t.stopRuntime(StatusStopped, nil)
	m.stopDetachedRuntime(id, t, runtime)
	return nil
}

func (m *Manager) Restart(id int) error {
	t := m.find(id)
	if t == nil {
		return fmt.Errorf("tunnel %d not found", id)
	}
	return m.startTunnel(t)
}

func (m *Manager) Delete(id int) error {
	m.mu.Lock()
	var removed *Tunnel
	for i, t := range m.tunnels {
		if t.ID() == id {
			removed = t
			m.tunnels = append(m.tunnels[:i], m.tunnels[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
	if removed == nil {
		return fmt.Errorf("tunnel %d not found", id)
	}
	runtime := removed.deleteRuntime()
	m.stopDetachedRuntime(id, removed, runtime)
	return nil
}

func (m *Manager) StopAll() {
	m.mu.RLock()
	tunnels := append([]*Tunnel(nil), m.tunnels...)
	m.mu.RUnlock()
	for _, t := range tunnels {
		id := t.ID()
		runtime := t.stopRuntime(StatusStopped, nil)
		m.stopDetachedRuntime(id, t, runtime)
	}
}

func (m *Manager) stopDetachedRuntime(id int, t *Tunnel, runtime detachedRuntime) {
	if runtime.cancel != nil {
		runtime.cancel()
	}
	if runtime.conn != nil {
		_ = runtime.conn.Stop()
		t.captureDetachedTotals(runtime)
	}
	m.mu.RLock()
	authCtx := m.authCtx
	m.mu.RUnlock()
	if authCtx != nil && authCtx.CancelRequests != nil {
		authCtx.CancelRequests(id)
	}
}

func (m *Manager) find(id int) *Tunnel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, t := range m.tunnels {
		if t.ID() == id {
			return t
		}
	}
	return nil
}

func (m *Manager) Get(id int) (Snapshot, bool) {
	t := m.find(id)
	if t == nil {
		return Snapshot{}, false
	}
	return t.Snapshot(), true
}

func (m *Manager) List() []Snapshot {
	m.mu.RLock()
	tunnels := append([]*Tunnel(nil), m.tunnels...)
	m.mu.RUnlock()
	snapshots := make([]Snapshot, 0, len(tunnels))
	for _, t := range tunnels {
		snapshots = append(snapshots, t.Snapshot())
	}
	return snapshots
}

func (m *Manager) Refresh(shouldPing bool, interval time.Duration) {
	m.mu.RLock()
	tunnels := append([]*Tunnel(nil), m.tunnels...)
	m.mu.RUnlock()
	for _, t := range tunnels {
		t.CheckStatus()
		t.refreshStats(shouldPing, interval)
	}
}

func (m *Manager) GetSFTPClient(id int) (interface{}, error) {
	t := m.find(id)
	if t == nil {
		return nil, fmt.Errorf("tunnel %d not found", id)
	}
	t.mu.RLock()
	status := t.status
	conn := t.conn
	t.mu.RUnlock()
	if status != StatusRunning {
		return nil, fmt.Errorf("tunnel %d is not running", id)
	}
	sc, ok := conn.(platform.SFTPCapable)
	if !ok {
		return nil, fmt.Errorf("tunnel %d does not support SFTP", id)
	}
	return sc.NewSFTPClient()
}
