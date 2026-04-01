package monitor

import (
	"context"
	"testing"
	"time"

	"github.com/spance/intun/internal/platform"
	"github.com/spance/intun/internal/tunnel"
)

func TestNewMonitor(t *testing.T) {
	m := tunnel.NewManager(nil)
	mon := NewMonitor(m, 1*time.Second)

	if mon.manager == nil {
		t.Error("manager should be set")
	}
	if mon.interval != 1*time.Second {
		t.Errorf("interval = %v, want 1s", mon.interval)
	}
	if mon.ctx == nil {
		t.Error("ctx should be initialized")
	}
}

func TestMonitorStartStop(t *testing.T) {
	mockExec := platform.NewMockExecutor()
	m := tunnel.NewManager(nil)
	m.SetExecutor(mockExec)

	mon := NewMonitor(m, 100*time.Millisecond)
	mon.Start()

	time.Sleep(250 * time.Millisecond)

	mon.Stop()

	if mon.ctx.Err() == nil {
		t.Error("context should be cancelled after Stop")
	}
}

func TestMonitorUpdatesStats(t *testing.T) {
	mockExec := platform.NewMockExecutor()
	mockConn := platform.NewMockConnection()
	mockExec.ConnectFn = func(cfg *platform.SSHConfig, tt platform.TunnelType, local, remote string) (*platform.MockConnection, error) {
		return mockConn, nil
	}

	m := tunnel.NewManager(nil)
	m.SetExecutor(mockExec)

	cfg := &platform.SSHConfig{Host: "example.com", Port: "22", User: "user"}
	tun, _ := m.Create("test", cfg, tunnel.Local, "8080", "80")

	mockConn.SetStats(1024, 2048)

	mon := NewMonitor(m, 50*time.Millisecond)
	mon.Start()

	time.Sleep(150 * time.Millisecond)

	mon.Stop()

	if tun.UploadBytes != 1024 {
		t.Errorf("UploadBytes = %d, want 1024", tun.UploadBytes)
	}
	if tun.DownloadBytes != 2048 {
		t.Errorf("DownloadBytes = %d, want 2048", tun.DownloadBytes)
	}
}

func TestMonitorPingDetection(t *testing.T) {
	mockExec := platform.NewMockExecutor()
	mockConn := platform.NewMockConnection()
	mockExec.ConnectFn = func(cfg *platform.SSHConfig, tt platform.TunnelType, local, remote string) (*platform.MockConnection, error) {
		return mockConn, nil
	}

	m := tunnel.NewManager(nil)
	m.SetExecutor(mockExec)

	cfg := &platform.SSHConfig{Host: "example.com", Port: "22", User: "user"}
	tun, _ := m.Create("test", cfg, tunnel.Local, "8080", "80")

	mockConn.SetPing(0)

	mon := NewMonitor(m, 20*time.Millisecond)
	mon.Start()

	for i := 0; i < pingIntervalMultiplier*3+2; i++ {
		time.Sleep(25 * time.Millisecond)
	}

	mon.Stop()

	if tun.GetStatus() != tunnel.StatusError {
		t.Errorf("Status = %v, want Error after ping failures", tun.GetStatus())
	}
	if tun.GetError() != "connection lost (ping timeout)" {
		t.Errorf("Error = %q, want 'connection lost (ping timeout)'", tun.GetError())
	}
}

func TestMonitorSuccessfulPing(t *testing.T) {
	mockExec := platform.NewMockExecutor()
	mockConn := platform.NewMockConnection()
	mockExec.ConnectFn = func(cfg *platform.SSHConfig, tt platform.TunnelType, local, remote string) (*platform.MockConnection, error) {
		return mockConn, nil
	}

	m := tunnel.NewManager(nil)
	m.SetExecutor(mockExec)

	cfg := &platform.SSHConfig{Host: "example.com", Port: "22", User: "user"}
	tun, _ := m.Create("test", cfg, tunnel.Local, "8080", "80")

	mockConn.SetPing(50 * time.Millisecond)

	mon := NewMonitor(m, 20*time.Millisecond)
	mon.Start()

	time.Sleep(200 * time.Millisecond)

	mon.Stop()

	if tun.GetStatus() != tunnel.StatusRunning {
		t.Errorf("Status = %v, want Running", tun.GetStatus())
	}
	if tun.Latency == 0 {
		t.Error("Latency should be updated from ping")
	}
}

func TestMonitorSkipsStoppedTunnels(t *testing.T) {
	mockExec := platform.NewMockExecutor()
	m := tunnel.NewManager(nil)
	m.SetExecutor(mockExec)

	cfg := &platform.SSHConfig{Host: "example.com", Port: "22", User: "user"}
	tun, _ := m.Create("test", cfg, tunnel.Local, "8080", "80")
	tun.Status = tunnel.StatusStopped

	mon := NewMonitor(m, 50*time.Millisecond)
	mon.Start()

	time.Sleep(100 * time.Millisecond)

	mon.Stop()

	if tun.UploadBytes != 0 {
		t.Error("Stopped tunnel should not update stats")
	}
}

func TestMonitorHandlesConnectionFailure(t *testing.T) {
	mockExec := platform.NewMockExecutor()
	mockConn := platform.NewMockConnection()
	mockExec.ConnectFn = func(cfg *platform.SSHConfig, tt platform.TunnelType, local, remote string) (*platform.MockConnection, error) {
		return mockConn, nil
	}

	m := tunnel.NewManager(nil)
	m.SetExecutor(mockExec)

	cfg := &platform.SSHConfig{Host: "example.com", Port: "22", User: "user"}
	tun, _ := m.Create("test", cfg, tunnel.Local, "8080", "80")

	mockConn.SetRunning(false)

	mon := NewMonitor(m, 50*time.Millisecond)
	mon.Start()

	time.Sleep(150 * time.Millisecond)

	mon.Stop()

	if tun.GetStatus() != tunnel.StatusError {
		t.Errorf("Status = %v, want Error", tun.GetStatus())
	}
}

func TestPingIntervalMultiplier(t *testing.T) {
	if pingIntervalMultiplier != 5 {
		t.Errorf("pingIntervalMultiplier = %d, want 5", pingIntervalMultiplier)
	}
}

func TestMonitorMultipleTunnels(t *testing.T) {
	mockExec := platform.NewMockExecutor()
	m := tunnel.NewManager(nil)
	m.SetExecutor(mockExec)

	cfg := &platform.SSHConfig{Host: "example.com", Port: "22", User: "user"}
	tun1, _ := m.Create("t1", cfg, tunnel.Local, "8080", "80")
	tun2, _ := m.Create("t2", cfg, tunnel.Local, "9090", "90")

	conn1 := platform.NewMockConnection()
	conn2 := platform.NewMockConnection()
	conn1.SetStats(100, 200)
	conn2.SetStats(300, 400)

	tun1.Conn = conn1
	tun2.Conn = conn2

	mon := NewMonitor(m, 50*time.Millisecond)
	mon.Start()

	time.Sleep(150 * time.Millisecond)

	mon.Stop()

	if tun1.UploadBytes != 100 {
		t.Errorf("tun1.UploadBytes = %d, want 100", tun1.UploadBytes)
	}
	if tun2.UploadBytes != 300 {
		t.Errorf("tun2.UploadBytes = %d, want 300", tun2.UploadBytes)
	}
}

func TestMonitorContextCancellation(t *testing.T) {
	m := tunnel.NewManager(nil)

	ctx, cancel := context.WithCancel(context.Background())
	mon := &Monitor{
		manager:  m,
		interval: 10 * time.Millisecond,
		ctx:      ctx,
		cancel:   cancel,
	}

	mon.Start()

	cancel()

	time.Sleep(50 * time.Millisecond)

	mon.wg.Wait()
}

func TestMonitorCalculatesSpeed(t *testing.T) {
	mockExec := platform.NewMockExecutor()
	mockConn := platform.NewMockConnection()
	mockExec.ConnectFn = func(cfg *platform.SSHConfig, tt platform.TunnelType, local, remote string) (*platform.MockConnection, error) {
		return mockConn, nil
	}

	m := tunnel.NewManager(nil)
	m.SetExecutor(mockExec)

	cfg := &platform.SSHConfig{Host: "example.com", Port: "22", User: "user"}
	tun, _ := m.Create("test", cfg, tunnel.Local, "8080", "80")
	tun.Conn = mockConn

	mockConn.SetStats(0, 0)

	mon := NewMonitor(m, 100*time.Millisecond)
	mon.Start()

	time.Sleep(120 * time.Millisecond)

	mockConn.SetStats(5000, 10000)

	time.Sleep(150 * time.Millisecond)

	mon.Stop()

	if tun.UploadBytes != 5000 {
		t.Errorf("UploadBytes = %d, want 5000", tun.UploadBytes)
	}
	if tun.DownloadBytes != 10000 {
		t.Errorf("DownloadBytes = %d, want 10000", tun.DownloadBytes)
	}

	if tun.UploadSpeed <= 0 && tun.UploadBytes > 0 {
		t.Errorf("UploadSpeed should be positive when traffic increased, got %d", tun.UploadSpeed)
	}
	if tun.DownloadSpeed <= 0 && tun.DownloadBytes > 0 {
		t.Errorf("DownloadSpeed should be positive when traffic increased, got %d", tun.DownloadSpeed)
	}
}
