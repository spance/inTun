package testutil

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spance/intun/internal/platform"
)

func TestMockConnectionLifecycleAndStats(t *testing.T) {
	conn := NewMockConnection()
	var stops int
	conn.OnStop = func() { stops++ }
	conn.SetStats(7, 9)
	conn.SetPing(15 * time.Millisecond)

	if !conn.IsRunning() {
		t.Fatal("new connection should be running")
	}
	up, down := conn.GetStats()
	if up != 7 || down != 9 || conn.Ping() != 15*time.Millisecond {
		t.Fatalf("stats=%d/%d ping=%v", up, down, conn.Ping())
	}
	conn.SetError("failed")
	if conn.IsRunning() || conn.Error() != "failed" {
		t.Fatalf("error state = running:%v error:%q", conn.IsRunning(), conn.Error())
	}
	conn.SetRunning(true)
	conn.OnIsRunning = func() bool { return false }
	if conn.IsRunning() {
		t.Fatal("running override was not used")
	}
	conn.OnIsRunning = nil
	if err := conn.Stop(); err != nil {
		t.Fatal(err)
	}
	_ = conn.Stop()
	if stops != 1 {
		t.Fatalf("stop callback count = %d, want 1", stops)
	}
}

func TestMockExecutorModes(t *testing.T) {
	exec := NewMockExecutor()
	cfg := &platform.SSHConfig{Host: "example.com"}
	spec := platform.ForwardSpec{Type: platform.Local, Protocol: platform.TCP, LocalAddr: "1", RemoteAddr: "2"}
	conn, err := exec.Connect(nil, cfg, spec)
	if err != nil || conn == nil || exec.GetCallCount() != 1 || exec.GetLastConnection() == nil {
		t.Fatalf("default connect = conn:%v err:%v calls:%d", conn, err, exec.GetCallCount())
	}

	exec.ConnectFn = func(gotCfg *platform.SSHConfig, gotSpec platform.ForwardSpec) (*MockConnection, error) {
		if gotCfg != cfg || gotSpec != spec {
			return nil, errors.New("unexpected arguments")
		}
		return NewMockConnection(), nil
	}
	if _, err := exec.Connect(nil, cfg, spec); err != nil {
		t.Fatal(err)
	}
	exec.ConnectFn = nil
	exec.ConnectContextFn = func(*platform.AuthContext, *platform.SSHConfig, platform.ForwardSpec) (*MockConnection, error) {
		return nil, ErrAuthFailed
	}
	if _, err := exec.Connect(nil, cfg, spec); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("context connect error = %v", err)
	}
	exec.ConnectContextFn = nil
	exec.ConnectErr = ErrConnectFailed
	if _, err := exec.Connect(nil, cfg, spec); !errors.Is(err, ErrConnectFailed) {
		t.Fatalf("configured connect error = %v", err)
	}
	if !strings.Contains(ErrHostKeyNotCached.Error(), "HOST_KEY_NOT_CACHED") {
		t.Fatalf("host key mock error = %v", ErrHostKeyNotCached)
	}
}
