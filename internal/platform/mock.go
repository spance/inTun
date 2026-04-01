package platform

import (
	"errors"
	"sync"
	"time"
)

type MockConnection struct {
	running     bool
	err         string
	upload      int64
	download    int64
	pingResult  time.Duration
	mu          sync.RWMutex
	stopOnce    sync.Once
	OnStop      func()
	OnIsRunning func() bool
}

func NewMockConnection() *MockConnection {
	return &MockConnection{
		running: true,
	}
}

func (m *MockConnection) Stop() error {
	m.stopOnce.Do(func() {
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
		if m.OnStop != nil {
			m.OnStop()
		}
	})
	return nil
}

func (m *MockConnection) IsRunning() bool {
	if m.OnIsRunning != nil {
		return m.OnIsRunning()
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

func (m *MockConnection) Error() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.err
}

func (m *MockConnection) WaitForReady(timeout time.Duration) bool {
	return m.running
}

func (m *MockConnection) GetStats() (int64, int64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.upload, m.download
}

func (m *MockConnection) Ping() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pingResult
}

func (m *MockConnection) SetRunning(r bool) {
	m.mu.Lock()
	m.running = r
	m.mu.Unlock()
}

func (m *MockConnection) SetError(e string) {
	m.mu.Lock()
	m.err = e
	if e != "" {
		m.running = false
	}
	m.mu.Unlock()
}

func (m *MockConnection) SetStats(up, down int64) {
	m.mu.Lock()
	m.upload = up
	m.download = down
	m.mu.Unlock()
}

func (m *MockConnection) SetPing(d time.Duration) {
	m.mu.Lock()
	m.pingResult = d
	m.mu.Unlock()
}

type MockExecutor struct {
	ConnectErr       error
	Connections      []*MockConnection
	ConnectFn        func(cfg *SSHConfig, tunnelType TunnelType, localPort, remotePort string) (*MockConnection, error)
	mu               sync.Mutex
	connectCallCount int
}

func NewMockExecutor() *MockExecutor {
	return &MockExecutor{}
}

func (e *MockExecutor) Connect(ctx *AuthContext, cfg *SSHConfig, tunnelType TunnelType, localPort, remotePort string) (Connection, error) {
	e.mu.Lock()
	e.connectCallCount++
	e.mu.Unlock()

	if e.ConnectErr != nil {
		return nil, e.ConnectErr
	}

	if e.ConnectFn != nil {
		return e.ConnectFn(cfg, tunnelType, localPort, remotePort)
	}

	conn := NewMockConnection()
	e.mu.Lock()
	e.Connections = append(e.Connections, conn)
	e.mu.Unlock()

	return conn, nil
}

func (e *MockExecutor) GetCallCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.connectCallCount
}

func (e *MockExecutor) GetLastConnection() *MockConnection {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.Connections) == 0 {
		return nil
	}
	return e.Connections[len(e.Connections)-1]
}

var ErrConnectFailed = errors.New("SSH_CONNECTION_FAILED: mock error")
var ErrAuthFailed = errors.New("SSH_AUTH_FAILED: mock error")
var ErrHostKeyNotCached = errors.New("HOST_KEY_NOT_CACHED: mock error")
