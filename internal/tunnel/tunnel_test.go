package tunnel

import (
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

func TestTunnelPingFailDetection(t *testing.T) {
	tun := &Tunnel{
		ID:     1,
		Status: StatusRunning,
	}

	for i := 0; i < 3; i++ {
		tun.UpdateStats(0, 0, 0, 0, 0, true)
	}

	if tun.Status != StatusError {
		t.Errorf("Status = %v after 3 ping failures, want %v", tun.Status, StatusError)
	}
	if tun.Error != "connection lost (ping timeout)" {
		t.Errorf("Error = %q, want %q", tun.Error, "connection lost (ping timeout)")
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
