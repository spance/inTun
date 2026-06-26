package tunnel

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/spance/intun/internal/platform"
)

func TestStatusString(t *testing.T) {
	tests := []struct {
		status Status
		want   string
	}{
		{StatusRunning, "Running"},
		{StatusStopped, "Stopped"},
		{StatusConnecting, "Connecting"},
		{StatusError, "Error"},
		{Status(99), "Unknown"},
	}
	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Errorf("Status(%d).String() = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestTunnelTypeString(t *testing.T) {
	tests := []struct {
		tt   TunnelType
		want string
	}{
		{Local, "Local"},
		{Remote, "Remote"},
		{Dynamic, "Dynamic"},
		{TunnelType(99), "Unknown"},
	}
	for _, tt := range tests {
		if got := tt.tt.String(); got != tt.want {
			t.Errorf("TunnelType(%d).String() = %q, want %q", tt.tt, got, tt.want)
		}
	}
}

func newTestManager(executor platform.Executor) *Manager {
	m := NewManager(nil)
	m.mu.Lock()
	m.executor = executor
	m.mu.Unlock()
	return m
}

func TestManagerCreate(t *testing.T) {
	mockExec := platform.NewMockExecutor()
	m := newTestManager(mockExec)

	cfg := &platform.SSHConfig{
		Host: "example.com",
		Port: "22",
		User: "testuser",
	}

	tunnel, err := m.Create("test-tunnel", cfg, Local, "8080", "80")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if tunnel.ID != 1 {
		t.Errorf("tunnel.ID = %d, want 1", tunnel.ID)
	}
	if tunnel.Name != "test-tunnel" {
		t.Errorf("tunnel.Name = %q, want %q", tunnel.Name, "test-tunnel")
	}
	if tunnel.Status != StatusRunning {
		t.Errorf("tunnel.Status = %v, want %v", tunnel.Status, StatusRunning)
	}
	if mockExec.GetCallCount() != 1 {
		t.Errorf("Connect called %d times, want 1", mockExec.GetCallCount())
	}
}

func TestManagerCreateError(t *testing.T) {
	mockExec := platform.NewMockExecutor()
	mockExec.ConnectErr = platform.ErrConnectFailed
	m := newTestManager(mockExec)

	cfg := &platform.SSHConfig{Host: "example.com"}

	tunnel, err := m.Create("test", cfg, Local, "8080", "80")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if tunnel.Status != StatusError {
		t.Errorf("tunnel.Status = %v, want %v", tunnel.Status, StatusError)
	}
	if tunnel.Error == "" {
		t.Error("tunnel.Error is empty")
	}
}

func TestManagerList(t *testing.T) {
	mockExec := platform.NewMockExecutor()
	m := newTestManager(mockExec)

	cfg := &platform.SSHConfig{Host: "host1"}
	m.Create("t1", cfg, Local, "8080", "80")

	cfg2 := &platform.SSHConfig{Host: "host2"}
	m.Create("t2", cfg2, Remote, "9090", "90")

	list := m.List()
	if len(list) != 2 {
		t.Errorf("List returned %d tunnels, want 2", len(list))
	}
}

func TestManagerStop(t *testing.T) {
	mockExec := platform.NewMockExecutor()
	m := newTestManager(mockExec)

	cfg := &platform.SSHConfig{Host: "example.com"}
	tunnel, _ := m.Create("test", cfg, Local, "8080", "80")

	err := m.Stop(tunnel.ID)
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if tunnel.Status != StatusStopped {
		t.Errorf("tunnel.Status = %v, want %v", tunnel.Status, StatusStopped)
	}
}

func TestManagerStopNotFound(t *testing.T) {
	m := NewManager(nil)

	err := m.Stop(999)
	if err == nil {
		t.Fatal("expected error for non-existent tunnel")
	}
}

func TestManagerDelete(t *testing.T) {
	mockExec := platform.NewMockExecutor()
	m := newTestManager(mockExec)

	cfg := &platform.SSHConfig{Host: "example.com"}
	tunnel, _ := m.Create("test", cfg, Local, "8080", "80")

	err := m.Delete(tunnel.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if len(m.List()) != 0 {
		t.Errorf("List returned %d tunnels after delete, want 0", len(m.List()))
	}
}

func TestManagerRestart(t *testing.T) {
	mockExec := platform.NewMockExecutor()
	m := newTestManager(mockExec)

	cfg := &platform.SSHConfig{Host: "example.com"}
	tunnel, _ := m.Create("test", cfg, Local, "8080", "80")

	m.Stop(tunnel.ID)

	err := m.Restart(tunnel.ID)
	if err != nil {
		t.Fatalf("Restart failed: %v", err)
	}

	if tunnel.Status != StatusRunning {
		t.Errorf("tunnel.Status = %v after restart, want %v", tunnel.Status, StatusRunning)
	}

	if mockExec.GetCallCount() != 2 {
		t.Errorf("Connect called %d times, want 2 (create + restart)", mockExec.GetCallCount())
	}
}

func TestTunnelUpdateStats(t *testing.T) {
	tun := &Tunnel{
		ID:     1,
		Status: StatusRunning,
	}

	tun.UpdateStats(1000, 2000, 100, 200, 50*time.Millisecond, true)

	if tun.UploadBytes != 1000 {
		t.Errorf("UploadBytes = %d, want 1000", tun.UploadBytes)
	}
	if tun.DownloadBytes != 2000 {
		t.Errorf("DownloadBytes = %d, want 2000", tun.DownloadBytes)
	}
	if tun.Latency != 50*time.Millisecond {
		t.Errorf("Latency = %v, want 50ms", tun.Latency)
	}
}

func TestTunnelCheckStatusDetectsConnectionFailure(t *testing.T) {
	mockConn := platform.NewMockConnection()
	tun := &Tunnel{
		ID:     1,
		Status: StatusRunning,
		Conn:   mockConn,
	}

	tun.CheckStatus()
	if tun.Status != StatusRunning {
		t.Errorf("Status should remain Running when connection is running")
	}

	mockConn.SetError("SSH_KEEPALIVE_FAILED: connection reset")

	tun.CheckStatus()

	if tun.Status != StatusError {
		t.Errorf("Status = %v after connection error, want %v", tun.Status, StatusError)
	}
	if tun.Error == "" {
		t.Error("Error should be set")
	}
}

func TestTunnelCheckStatus(t *testing.T) {
	mockConn := platform.NewMockConnection()
	tun := &Tunnel{
		ID:     1,
		Status: StatusRunning,
		Conn:   mockConn,
	}

	tun.CheckStatus()
	if tun.Status != StatusRunning {
		t.Errorf("Status should remain Running when connection is running")
	}

	mockConn.SetRunning(false)
	tun.CheckStatus()

	if tun.Status != StatusError {
		t.Errorf("Status = %v after connection stopped, want %v", tun.Status, StatusError)
	}
}

func TestTunnelGetStatusAndError(t *testing.T) {
	tun := &Tunnel{
		ID:     1,
		Status: StatusError,
		Error:  "test error",
	}

	if diff := cmp.Diff(tun.GetStatus(), StatusError); diff != "" {
		t.Errorf("GetStatus() mismatch (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(tun.GetError(), "test error"); diff != "" {
		t.Errorf("GetError() mismatch (-want +got):\n%s", diff)
	}
}

func TestManagerGetReturnsMatchingTunnel(t *testing.T) {
	mockExec := platform.NewMockExecutor()
	m := newTestManager(mockExec)
	cfg := &platform.SSHConfig{Host: "example.com"}

	first, err := m.Create("first", cfg, Local, "8080", "80")
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Create("second", cfg, Remote, "9090", "90")
	if err != nil {
		t.Fatal(err)
	}

	if got := m.Get(second.ID); got != second {
		t.Fatalf("Get(%d) = %#v, want second tunnel", second.ID, got)
	}
	if got := m.Get(first.ID); got != first {
		t.Fatalf("Get(%d) = %#v, want first tunnel", first.ID, got)
	}
	if got := m.Get(999); got != nil {
		t.Fatalf("Get missing tunnel = %#v, want nil", got)
	}
}

func TestTunnelGetSnapshotReturnsAtomicStats(t *testing.T) {
	tun := &Tunnel{}
	tun.UpdateStats(123, 456, 7, 8, 25*time.Millisecond, true)

	got := tun.GetSnapshot()
	if got.UploadBytes != 123 || got.DownloadBytes != 456 {
		t.Fatalf("GetSnapshot = %#v, want upload 123 download 456", got)
	}
}

type testSFTPConnection struct {
	*platform.MockConnection
	client interface{}
	err    error
}

func (c *testSFTPConnection) NewSFTPClient() (interface{}, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.client, nil
}

func TestManagerGetSFTPClient(t *testing.T) {
	wantClient := struct{ name string }{name: "sftp"}
	conn := &testSFTPConnection{MockConnection: platform.NewMockConnection(), client: wantClient}
	m := newTestManager(platform.NewMockExecutor())
	m.Tunnels = append(m.Tunnels, &Tunnel{ID: 1, Status: StatusRunning, Conn: conn})

	got, err := m.GetSFTPClient(1)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantClient {
		t.Fatalf("GetSFTPClient = %#v, want %#v", got, wantClient)
	}
}

func TestManagerGetSFTPClientRejectsInvalidStates(t *testing.T) {
	m := newTestManager(platform.NewMockExecutor())
	m.Tunnels = append(m.Tunnels,
		&Tunnel{ID: 1, Status: StatusStopped, Conn: &testSFTPConnection{MockConnection: platform.NewMockConnection()}},
		&Tunnel{ID: 2, Status: StatusRunning, Conn: platform.NewMockConnection()},
		&Tunnel{ID: 3, Status: StatusRunning, Conn: &testSFTPConnection{MockConnection: platform.NewMockConnection(), err: fmt.Errorf("sftp failed")}},
	)

	tests := []struct {
		id   int
		want string
	}{
		{id: 1, want: "is not running"},
		{id: 2, want: "does not support SFTP"},
		{id: 3, want: "sftp failed"},
		{id: 99, want: "not found"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("id-%d", tt.id), func(t *testing.T) {
			_, err := m.GetSFTPClient(tt.id)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("GetSFTPClient(%d) error = %v, want containing %q", tt.id, err, tt.want)
			}
		})
	}
}

func TestManagerSettersAffectCreate(t *testing.T) {
	authCtx := &platform.AuthContext{}
	mockExec := platform.NewMockExecutor()
	m := NewManager(nil)
	m.SetAuthContext(authCtx)
	m.SetExecutor(mockExec)

	cfg := &platform.SSHConfig{Host: "example.com"}
	if _, err := m.Create("test", cfg, Local, "8080", "80"); err != nil {
		t.Fatal(err)
	}
	if mockExec.GetCallCount() != 1 {
		t.Fatalf("custom executor call count = %d, want 1", mockExec.GetCallCount())
	}
}
