package tunnel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/spance/intun/internal/platform"
	"github.com/spance/intun/internal/testutil"
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
	mockExec := testutil.NewMockExecutor()
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

	snapshot := tunnel.Snapshot()
	if snapshot.ID != 1 {
		t.Errorf("tunnel.ID = %d, want 1", snapshot.ID)
	}
	if snapshot.Name != "test-tunnel" {
		t.Errorf("tunnel.Name = %q, want %q", snapshot.Name, "test-tunnel")
	}
	if snapshot.Status != StatusRunning {
		t.Errorf("tunnel.Status = %v, want %v", snapshot.Status, StatusRunning)
	}
	if mockExec.GetCallCount() != 1 {
		t.Errorf("Connect called %d times, want 1", mockExec.GetCallCount())
	}
}

func TestManagerCreateError(t *testing.T) {
	mockExec := testutil.NewMockExecutor()
	mockExec.ConnectErr = testutil.ErrConnectFailed
	m := newTestManager(mockExec)

	cfg := &platform.SSHConfig{Host: "example.com"}

	tunnel, err := m.Create("test", cfg, Local, "8080", "80")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	snapshot := tunnel.Snapshot()
	if snapshot.Status != StatusError {
		t.Errorf("tunnel.Status = %v, want %v", snapshot.Status, StatusError)
	}
	if snapshot.Error == "" {
		t.Error("tunnel.Error is empty")
	}
}

func TestManagerCreateKeepsAsyncConnectionConnectingUntilReady(t *testing.T) {
	conn := testutil.NewMockConnection()
	conn.SetRunning(false)
	mockExec := testutil.NewMockExecutor()
	mockExec.ConnectFn = func(cfg *platform.SSHConfig, spec platform.ForwardSpec) (*testutil.MockConnection, error) {
		return conn, nil
	}
	m := newTestManager(mockExec)

	tun, err := m.Create("slow", &platform.SSHConfig{Host: "example.com"}, Local, "8080", "80")
	if err != nil {
		t.Fatalf("Create returned error for in-progress connection: %v", err)
	}
	if tun.GetStatus() != StatusConnecting {
		t.Fatalf("initial status = %v, want Connecting", tun.GetStatus())
	}

	conn.SetRunning(true)
	tun.CheckStatus()

	if tun.GetStatus() != StatusRunning {
		t.Fatalf("ready status = %v, want Running", tun.GetStatus())
	}
}

func TestManagerCreateWithProtocolPassesForwardSpec(t *testing.T) {
	mockExec := testutil.NewMockExecutor()
	var captured platform.ForwardSpec
	mockExec.ConnectFn = func(cfg *platform.SSHConfig, spec platform.ForwardSpec) (*testutil.MockConnection, error) {
		captured = spec
		return testutil.NewMockConnection(), nil
	}
	m := newTestManager(mockExec)
	tun, err := m.CreateWithProtocol("udp", &platform.SSHConfig{Host: "example.com"}, Local, UDP, "127.0.0.1:5353", "127.0.0.1:53")
	if err != nil {
		t.Fatal(err)
	}
	if tun.ForwardSpec().Protocol != UDP || captured.Type != platform.Local || captured.Protocol != platform.UDP || captured.LocalAddr != "127.0.0.1:5353" || captured.RemoteAddr != "127.0.0.1:53" {
		t.Fatalf("captured forward = %#v, tunnel protocol = %s", captured, tun.ForwardSpec().Protocol)
	}
}

func TestManagerCreateAsyncConnectionFailureBecomesError(t *testing.T) {
	conn := testutil.NewMockConnection()
	conn.SetRunning(false)
	mockExec := testutil.NewMockExecutor()
	mockExec.ConnectFn = func(cfg *platform.SSHConfig, spec platform.ForwardSpec) (*testutil.MockConnection, error) {
		return conn, nil
	}
	m := newTestManager(mockExec)

	tun, err := m.Create("slow-fail", &platform.SSHConfig{Host: "example.com"}, Local, "8080", "80")
	if err != nil {
		t.Fatalf("Create returned error for in-progress connection: %v", err)
	}

	conn.SetError("SSH_CONNECTION_FAILED: timeout")
	tun.CheckStatus()

	if tun.GetStatus() != StatusError {
		t.Fatalf("failed status = %v, want Error", tun.GetStatus())
	}
	if !strings.Contains(tun.GetError(), "timeout") {
		t.Fatalf("error = %q, want timeout", tun.GetError())
	}
}

func TestManagerList(t *testing.T) {
	mockExec := testutil.NewMockExecutor()
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
	mockExec := testutil.NewMockExecutor()
	m := newTestManager(mockExec)

	cfg := &platform.SSHConfig{Host: "example.com"}
	tunnel, _ := m.Create("test", cfg, Local, "8080", "80")

	err := m.Stop(tunnel.ID())
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if tunnel.GetStatus() != StatusStopped {
		t.Errorf("tunnel.Status = %v, want %v", tunnel.GetStatus(), StatusStopped)
	}
}

func TestStopAndRestartCaptureFinalConnectionTotals(t *testing.T) {
	mockExec := testutil.NewMockExecutor()
	m := newTestManager(mockExec)
	tun, err := m.Create("test", &platform.SSHConfig{Host: "example.com"}, Local, "8080", "80")
	if err != nil {
		t.Fatal(err)
	}
	first := mockExec.GetLastConnection()
	first.SetStats(100, 200)
	if err := m.Restart(tun.ID()); err != nil {
		t.Fatal(err)
	}
	second := mockExec.GetLastConnection()
	second.SetStats(25, 50)
	m.Refresh(false, time.Second)
	snapshot, ok := m.Get(tun.ID())
	if !ok || snapshot.UploadBytes != 125 || snapshot.DownloadBytes != 250 {
		t.Fatalf("restart totals = %#v, want 125/250", snapshot)
	}
	second.SetStats(40, 80)
	if err := m.Stop(tun.ID()); err != nil {
		t.Fatal(err)
	}
	snapshot, _ = m.Get(tun.ID())
	if snapshot.UploadBytes != 140 || snapshot.DownloadBytes != 280 {
		t.Fatalf("stop totals = %d/%d, want 140/280", snapshot.UploadBytes, snapshot.DownloadBytes)
	}
}

func TestStopCapturesBytesProducedUntilConnectionCloses(t *testing.T) {
	conn := testutil.NewMockConnection()
	conn.SetStats(100, 200)
	conn.OnStop = func() {
		conn.SetStats(125, 250)
	}
	exec := testutil.NewMockExecutor()
	exec.ConnectFn = func(*platform.SSHConfig, platform.ForwardSpec) (*testutil.MockConnection, error) {
		return conn, nil
	}
	m := newTestManager(exec)
	tun, err := m.Create("test", &platform.SSHConfig{Host: "example.com"}, Local, "8080", "80")
	if err != nil {
		t.Fatal(err)
	}
	m.Refresh(false, time.Second)

	if err := m.Stop(tun.ID()); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := m.Get(tun.ID())
	if snapshot.UploadBytes != 125 || snapshot.DownloadBytes != 250 {
		t.Fatalf("final totals = %d/%d, want 125/250", snapshot.UploadBytes, snapshot.DownloadBytes)
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
	mockExec := testutil.NewMockExecutor()
	m := newTestManager(mockExec)

	cfg := &platform.SSHConfig{Host: "example.com"}
	tunnel, _ := m.Create("test", cfg, Local, "8080", "80")

	err := m.Delete(tunnel.ID())
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if len(m.List()) != 0 {
		t.Errorf("List returned %d tunnels after delete, want 0", len(m.List()))
	}
}

func TestManagerRestart(t *testing.T) {
	mockExec := testutil.NewMockExecutor()
	m := newTestManager(mockExec)

	cfg := &platform.SSHConfig{Host: "example.com"}
	tunnel, _ := m.Create("test", cfg, Local, "8080", "80")

	m.Stop(tunnel.ID())

	err := m.Restart(tunnel.ID())
	if err != nil {
		t.Fatalf("Restart failed: %v", err)
	}

	if tunnel.GetStatus() != StatusRunning {
		t.Errorf("tunnel.Status = %v after restart, want %v", tunnel.GetStatus(), StatusRunning)
	}

	if mockExec.GetCallCount() != 2 {
		t.Errorf("Connect called %d times, want 2 (create + restart)", mockExec.GetCallCount())
	}
}

func TestTunnelUpdateStats(t *testing.T) {
	conn := testutil.NewMockConnection()
	conn.SetStats(1000, 2000)
	tun := &Tunnel{
		id:     1,
		status: StatusRunning,
		conn:   conn,
	}

	conn.SetPing(50 * time.Millisecond)
	tun.refreshStats(true, time.Second)
	snapshot := tun.Snapshot()

	if snapshot.UploadBytes != 1000 {
		t.Errorf("UploadBytes = %d, want 1000", snapshot.UploadBytes)
	}
	if snapshot.DownloadBytes != 2000 {
		t.Errorf("DownloadBytes = %d, want 2000", snapshot.DownloadBytes)
	}
	if snapshot.Latency != 50*time.Millisecond {
		t.Errorf("Latency = %v, want 50ms", snapshot.Latency)
	}
}

func TestTunnelCheckStatusDetectsConnectionFailure(t *testing.T) {
	mockConn := testutil.NewMockConnection()
	tun := &Tunnel{
		id:     1,
		status: StatusRunning,
		conn:   mockConn,
	}

	tun.CheckStatus()
	if tun.GetStatus() != StatusRunning {
		t.Errorf("Status should remain Running when connection is running")
	}

	mockConn.SetError("SSH_KEEPALIVE_FAILED: connection reset")

	tun.CheckStatus()

	if tun.GetStatus() != StatusError {
		t.Errorf("Status = %v after connection error, want %v", tun.GetStatus(), StatusError)
	}
	if tun.GetError() == "" {
		t.Error("Error should be set")
	}
}

func TestTunnelCheckStatus(t *testing.T) {
	mockConn := testutil.NewMockConnection()
	tun := &Tunnel{
		id:     1,
		status: StatusRunning,
		conn:   mockConn,
	}

	tun.CheckStatus()
	if tun.GetStatus() != StatusRunning {
		t.Errorf("Status should remain Running when connection is running")
	}

	mockConn.SetRunning(false)
	tun.CheckStatus()

	if tun.GetStatus() != StatusError {
		t.Errorf("Status = %v after connection stopped, want %v", tun.GetStatus(), StatusError)
	}
}

func TestTunnelGetStatusAndError(t *testing.T) {
	tun := &Tunnel{
		id:      1,
		status:  StatusError,
		failure: &platform.Failure{Code: platform.FailureUnknown, Detail: "test error"},
	}

	if diff := cmp.Diff(tun.GetStatus(), StatusError); diff != "" {
		t.Errorf("GetStatus() mismatch (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(tun.GetError(), "test error"); diff != "" {
		t.Errorf("GetError() mismatch (-want +got):\n%s", diff)
	}
}

func TestManagerGetReturnsMatchingTunnel(t *testing.T) {
	mockExec := testutil.NewMockExecutor()
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

	if got, ok := m.Get(second.ID()); !ok || got.ID != second.ID() {
		t.Fatalf("Get(%d) = %#v, want second tunnel", second.ID(), got)
	}
	if got, ok := m.Get(first.ID()); !ok || got.ID != first.ID() {
		t.Fatalf("Get(%d) = %#v, want first tunnel", first.ID(), got)
	}
	if got, ok := m.Get(999); ok {
		t.Fatalf("Get missing tunnel = %#v, want nil", got)
	}
}

func TestTunnelGetSnapshotReturnsAtomicStats(t *testing.T) {
	tun := &Tunnel{uploadBytes: 123, downloadBytes: 456}

	got := tun.GetSnapshot()
	if got.UploadBytes != 123 || got.DownloadBytes != 456 {
		t.Fatalf("GetSnapshot = %#v, want upload 123 download 456", got)
	}
}

type testSFTPConnection struct {
	*testutil.MockConnection
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
	conn := &testSFTPConnection{MockConnection: testutil.NewMockConnection(), client: wantClient}
	m := newTestManager(testutil.NewMockExecutor())
	m.tunnels = append(m.tunnels, &Tunnel{id: 1, status: StatusRunning, conn: conn})

	got, err := m.GetSFTPClient(1)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantClient {
		t.Fatalf("GetSFTPClient = %#v, want %#v", got, wantClient)
	}
}

func TestManagerGetSFTPClientRejectsInvalidStates(t *testing.T) {
	m := newTestManager(testutil.NewMockExecutor())
	m.tunnels = append(m.tunnels,
		&Tunnel{id: 1, status: StatusStopped, conn: &testSFTPConnection{MockConnection: testutil.NewMockConnection()}},
		&Tunnel{id: 2, status: StatusRunning, conn: testutil.NewMockConnection()},
		&Tunnel{id: 3, status: StatusRunning, conn: &testSFTPConnection{MockConnection: testutil.NewMockConnection(), err: fmt.Errorf("sftp failed")}},
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
	mockExec := testutil.NewMockExecutor()
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

func TestDeleteRejectsLateConnectionResult(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	conn := testutil.NewMockConnection()
	exec := testutil.NewMockExecutor()
	exec.ConnectContextFn = func(ctx *platform.AuthContext, cfg *platform.SSHConfig, spec platform.ForwardSpec) (*testutil.MockConnection, error) {
		close(started)
		<-release
		return conn, nil
	}
	m := newTestManager(exec)

	createDone := make(chan error, 1)
	go func() {
		_, err := m.Create("late", &platform.SSHConfig{Host: "example.com"}, Local, "8080", "80")
		createDone <- err
	}()
	<-started
	if err := m.Delete(1); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-createDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("late Create error = %v, want context cancellation", err)
	}
	if len(m.List()) != 0 {
		t.Fatal("deleted tunnel was restored by a late connection")
	}
	if conn.IsRunning() {
		t.Fatal("late connection was not stopped")
	}
}

func TestDeletedTunnelCannotBeStartedThroughStaleReference(t *testing.T) {
	exec := testutil.NewMockExecutor()
	m := newTestManager(exec)
	tun, err := m.Create("deleted", &platform.SSHConfig{Host: "example.com"}, Local, "8080", "80")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(tun.ID()); err != nil {
		t.Fatal(err)
	}

	if err := m.startTunnel(tun); !errors.Is(err, errTunnelDeleted) {
		t.Fatalf("start deleted tunnel error = %v, want %v", err, errTunnelDeleted)
	}
	if got := exec.GetCallCount(); got != 1 {
		t.Fatalf("Connect called %d times, want only the initial create", got)
	}
}

func TestRestartSupersedesBlockedConnectionGeneration(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstConn := testutil.NewMockConnection()
	secondConn := testutil.NewMockConnection()
	exec := testutil.NewMockExecutor()
	var call atomic.Int32
	exec.ConnectContextFn = func(ctx *platform.AuthContext, cfg *platform.SSHConfig, spec platform.ForwardSpec) (*testutil.MockConnection, error) {
		if call.Add(1) == 1 {
			close(firstStarted)
			<-releaseFirst
			return firstConn, nil
		}
		return secondConn, nil
	}
	m := newTestManager(exec)

	createDone := make(chan error, 1)
	go func() {
		_, err := m.Create("restart", &platform.SSHConfig{Host: "example.com"}, Local, "8080", "80")
		createDone <- err
	}()
	<-firstStarted
	if err := m.Restart(1); err != nil {
		t.Fatal(err)
	}
	close(releaseFirst)
	if err := <-createDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("superseded Create error = %v", err)
	}
	snapshot, ok := m.Get(1)
	if !ok || snapshot.Status != StatusRunning {
		t.Fatalf("restart snapshot = %#v", snapshot)
	}
	if firstConn.IsRunning() {
		t.Fatal("superseded connection was not stopped")
	}
	if !secondConn.IsRunning() {
		t.Fatal("current connection was stopped")
	}
}

func TestStopCancelsInFlightConnectContextAndAuthRequests(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	cancelledRequests := make(chan int, 2)
	authCtx := &platform.AuthContext{
		Cancel: context.Background(),
		CancelRequests: func(id int) {
			cancelledRequests <- id
		},
	}
	exec := testutil.NewMockExecutor()
	exec.ConnectContextFn = func(ctx *platform.AuthContext, cfg *platform.SSHConfig, spec platform.ForwardSpec) (*testutil.MockConnection, error) {
		close(started)
		<-ctx.Cancel.Done()
		close(cancelled)
		return nil, ctx.Cancel.Err()
	}
	m := NewManager(authCtx)
	m.SetExecutor(exec)

	createDone := make(chan error, 1)
	go func() {
		_, err := m.Create("cancel", &platform.SSHConfig{Host: "example.com"}, Local, "8080", "80")
		createDone <- err
	}()
	<-started
	if err := m.Stop(1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("connect context was not cancelled")
	}
	<-createDone
	select {
	case id := <-cancelledRequests:
		if id != 1 {
			t.Fatalf("cancelled auth request id = %d", id)
		}
	default:
		t.Fatal("auth prompt cancellation was not requested")
	}
}
