package platform

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestTCPForwardAddr(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{
			name: "plain port defaults to localhost",
			addr: "5551",
			want: "127.0.0.1:5551",
		},
		{
			name: "host and port are preserved",
			addr: "10.0.0.15:5551",
			want: "10.0.0.15:5551",
		},
		{
			name: "localhost and port are preserved",
			addr: "127.0.0.1:5551",
			want: "127.0.0.1:5551",
		},
		{
			name: "invalid port is still prefixed",
			addr: "not-a-port",
			want: "127.0.0.1:not-a-port",
		},
		{
			name: "overflow port is still prefixed",
			addr: "70000",
			want: "127.0.0.1:70000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tcpForwardAddr(tt.addr); got != tt.want {
				t.Fatalf("tcpForwardAddr(%q) = %q, want %q", tt.addr, got, tt.want)
			}
		})
	}
}

func TestSetConnectionLostDoesNotOverwriteExistingError(t *testing.T) {
	conn := &SSHConnection{}

	conn.setError("REMOTE_LISTEN_FAILED: bind: address already in use")
	conn.setConnectionLost("SSH_CONNECTION_LOST: use of closed network connection")

	if got := conn.Error(); got != "REMOTE_LISTEN_FAILED: bind: address already in use" {
		t.Fatalf("Error() = %q, want original remote listen error", got)
	}
	if conn.IsRunning() {
		t.Fatal("connection should not be running after setError")
	}
}

func TestSetConnectionLostStoresLossWhenNoEarlierError(t *testing.T) {
	conn := &SSHConnection{}

	conn.setConnectionLost("SSH_CONNECTION_LOST: EOF")

	if got := conn.Error(); got != "SSH_CONNECTION_LOST: EOF" {
		t.Fatalf("Error() = %q, want connection lost error", got)
	}
}

func TestTunnelTypeStringUnknownFallback(t *testing.T) {
	tests := []struct {
		tunnelType TunnelType
		want       string
	}{
		{Local, "Local"},
		{Remote, "Remote"},
		{Dynamic, "Dynamic"},
		{TunnelType(99), "Unknown"},
	}

	for _, tt := range tests {
		if got := tt.tunnelType.String(); got != tt.want {
			t.Fatalf("TunnelType(%d).String() = %q, want %q", tt.tunnelType, got, tt.want)
		}
	}
}

func TestNewExecutorReturnsSSHExecutor(t *testing.T) {
	if _, ok := NewExecutor().(*SSHExecutor); !ok {
		t.Fatal("NewExecutor should return *SSHExecutor")
	}
}

func TestSSHConnectionStateHelpers(t *testing.T) {
	conn := &SSHConnection{}
	if conn.IsRunning() {
		t.Fatal("new connection should not be running before it is ready")
	}
	conn.setReady()
	if !conn.IsRunning() {
		t.Fatal("setReady should mark connection running")
	}

	closer := &countingCloser{}
	conn.addForward(closer)
	conn.setExited()
	if conn.IsRunning() {
		t.Fatal("setExited should mark connection not running")
	}
	if len(conn.forwards) != 1 {
		t.Fatalf("forward count = %d, want 1", len(conn.forwards))
	}
	if err := conn.Stop(); err != nil {
		t.Fatal(err)
	}
	if closer.closed != 1 {
		t.Fatalf("forward close count = %d, want 1", closer.closed)
	}
	if err := conn.Stop(); err != nil {
		t.Fatal(err)
	}
	if closer.closed != 1 {
		t.Fatalf("Stop should be idempotent, close count = %d", closer.closed)
	}
}

func TestSSHConnectionStatsAndSFTPUnavailable(t *testing.T) {
	conn := &SSHConnection{}
	conn.totalUpload.Store(10)
	conn.totalDownload.Store(20)

	upload, download := conn.GetStats()
	if upload != 10 || download != 20 {
		t.Fatalf("GetStats = %d/%d, want 10/20", upload, download)
	}
	if latency := conn.Ping(); latency != 0 {
		t.Fatalf("Ping without client = %v, want 0", latency)
	}
	if _, err := conn.NewSFTPClient(); err == nil || !strings.Contains(err.Error(), "connection not available") {
		t.Fatalf("NewSFTPClient error = %v, want connection not available", err)
	}
}

func TestCountedConnTracksReadAndWrite(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	var upload, download atomic.Int64
	counted := NewCountedConn(client, &upload, &download)

	readDone := make(chan error, 1)
	go func() {
		_, err := server.Write([]byte("hello"))
		readDone <- err
	}()

	buf := make([]byte, 5)
	n, err := counted.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 || string(buf) != "hello" || download.Load() != 5 {
		t.Fatalf("Read n=%d buf=%q download=%d, want hello/5", n, string(buf), download.Load())
	}
	if err := <-readDone; err != nil {
		t.Fatal(err)
	}

	writeDone := make(chan []byte, 1)
	go func() {
		data := make([]byte, 5)
		if _, err := io.ReadFull(server, data); err != nil {
			writeDone <- []byte("ERR:" + err.Error())
			return
		}
		writeDone <- data
	}()
	if n, err := counted.Write([]byte("world")); err != nil || n != 5 {
		t.Fatalf("Write n=%d err=%v, want 5 nil", n, err)
	}
	if got := string(<-writeDone); got != "world" {
		t.Fatalf("server read %q, want world", got)
	}
	if upload.Load() != 5 {
		t.Fatalf("upload = %d, want 5", upload.Load())
	}
}

func TestDynamicForwardReturnsFailureWithoutSSHClient(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	client.SetDeadline(time.Now().Add(time.Second))

	done := make(chan struct{})
	go func() {
		(&SSHConnection{}).handleDynamicForward(server)
		close(done)
	}()

	if _, err := client.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatal(err)
	}
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(client, greeting); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(greeting, []byte{0x05, 0x00}) {
		t.Fatalf("SOCKS greeting = %#v, want no-auth reply", greeting)
	}

	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len("example.com"))}
	req = append(req, []byte("example.com")...)
	req = append(req, 0x00, 0x50)
	if _, err := client.Write(req); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatal(err)
	}
	if reply[0] != 0x05 || reply[1] != 0x05 {
		t.Fatalf("SOCKS reply = %#v, want connection failure", reply)
	}
	client.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("dynamic forward handler did not exit")
	}
}

func TestDynamicForwardRejectsUnsupportedCommand(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	client.SetDeadline(time.Now().Add(time.Second))

	done := make(chan struct{})
	go func() {
		(&SSHConnection{}).handleDynamicForward(server)
		close(done)
	}()

	if _, err := client.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatal(err)
	}
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(client, greeting); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Write([]byte{0x05, 0x02, 0x00, 0x01, 127, 0, 0, 1, 0, 80}); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatal(err)
	}
	if reply[1] != 0x07 {
		t.Fatalf("SOCKS reply = %#v, want command-not-supported", reply)
	}
	client.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("dynamic forward handler did not exit")
	}
}

func TestProxyTCPTransfersBothDirections(t *testing.T) {
	leftProxy, leftPeer := net.Pipe()
	rightProxy, rightPeer := net.Pipe()
	leftPeer.SetDeadline(time.Now().Add(time.Second))
	rightPeer.SetDeadline(time.Now().Add(time.Second))

	done := make(chan struct{})
	go func() {
		proxyTCP(leftProxy, rightProxy)
		close(done)
	}()

	if _, err := leftPeer.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(rightPeer, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "ping" {
		t.Fatalf("right peer read %q, want ping", string(buf))
	}

	if _, err := rightPeer.Write([]byte("pong")); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(leftPeer, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "pong" {
		t.Fatalf("left peer read %q, want pong", string(buf))
	}

	leftPeer.Close()
	rightPeer.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("proxyTCP did not exit after peers closed")
	}
}

func TestStartLocalAndDynamicForwardListeners(t *testing.T) {
	exec := &SSHExecutor{}

	localConn := &SSHConnection{}
	exec.startLocalForward(localConn, "127.0.0.1:0", "80")
	if got := localConn.Error(); got != "" {
		t.Fatalf("local forward error = %q, want none", got)
	}
	if len(localConn.forwards) != 1 {
		t.Fatalf("local forward count = %d, want 1 listener", len(localConn.forwards))
	}
	if err := localConn.Stop(); err != nil {
		t.Fatal(err)
	}

	dynamicConn := &SSHConnection{}
	exec.startDynamicForward(dynamicConn, "127.0.0.1:0")
	if got := dynamicConn.Error(); got != "" {
		t.Fatalf("dynamic forward error = %q, want none", got)
	}
	if len(dynamicConn.forwards) != 1 {
		t.Fatalf("dynamic forward count = %d, want 1 listener", len(dynamicConn.forwards))
	}
	if err := dynamicConn.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestStartLocalForwardReportsListenFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	conn := &SSHConnection{}
	(&SSHExecutor{}).startLocalForward(conn, listener.Addr().String(), "80")

	if got := conn.Error(); !strings.Contains(got, "LISTEN_FAILED") {
		t.Fatalf("local forward error = %q, want LISTEN_FAILED", got)
	}
}

func TestSSHExecutorConnectEarlyFailures(t *testing.T) {
	exec := &SSHExecutor{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	conn, err := exec.Connect(&AuthContext{Cancel: ctx}, &SSHConfig{Host: "example.com"}, Local, "1", "2")
	if err != nil {
		t.Fatal(err)
	}
	waitForConnectionError(t, conn, "cancelled")

	conn, err = exec.Connect(nil, &SSHConfig{}, Local, "1", "2")
	if err != nil {
		t.Fatal(err)
	}
	waitForConnectionError(t, conn, "no host specified")
}

func TestMockConnectionAndExecutor(t *testing.T) {
	conn := NewMockConnection()
	var stopped int
	conn.OnStop = func() { stopped++ }
	if !conn.IsRunning() {
		t.Fatal("new mock connection should start running")
	}
	conn.SetError("boom")
	if conn.Error() != "boom" {
		t.Fatalf("mock error = %q, want boom", conn.Error())
	}
	if conn.IsRunning() {
		t.Fatal("SetError should mark mock connection stopped")
	}
	conn.SetRunning(true)
	if !conn.IsRunning() {
		t.Fatal("SetRunning(true) should mark mock connection running")
	}
	conn.OnIsRunning = func() bool { return false }
	if conn.IsRunning() {
		t.Fatal("OnIsRunning override should be used")
	}
	conn.OnIsRunning = nil
	conn.SetStats(7, 9)
	conn.SetPing(15 * time.Millisecond)
	up, down := conn.GetStats()
	if up != 7 || down != 9 {
		t.Fatalf("mock stats = %d/%d, want 7/9", up, down)
	}
	if got := conn.Ping(); got != 15*time.Millisecond {
		t.Fatalf("mock ping = %v, want 15ms", got)
	}
	if err := conn.Stop(); err != nil {
		t.Fatal(err)
	}
	_ = conn.Stop()
	if stopped != 1 {
		t.Fatalf("OnStop count = %d, want 1", stopped)
	}

	exec := NewMockExecutor()
	cfg := &SSHConfig{Host: "example.com"}
	gotConn, err := exec.Connect(nil, cfg, Local, "1", "2")
	if err != nil {
		t.Fatal(err)
	}
	if gotConn == nil || exec.GetCallCount() != 1 || exec.GetLastConnection() == nil {
		t.Fatalf("mock executor did not record connection")
	}

	exec.ConnectFn = func(cfg *SSHConfig, tunnelType TunnelType, localPort, remotePort string) (*MockConnection, error) {
		if cfg.Host != "example.com" || tunnelType != Remote || localPort != "3" || remotePort != "4" {
			return nil, errors.New("unexpected args")
		}
		return NewMockConnection(), nil
	}
	if _, err := exec.Connect(nil, cfg, Remote, "3", "4"); err != nil {
		t.Fatal(err)
	}

	exec.ConnectErr = ErrAuthFailed
	if _, err := exec.Connect(nil, cfg, Local, "", ""); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("Connect error = %v, want ErrAuthFailed", err)
	}
}

func TestNewKnownHostsCreatesFileAndAcceptsUnknownHost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	kh, err := NewKnownHosts()
	if err != nil {
		t.Fatal(err)
	}
	key := testPublicKey(t)
	reqCh := make(chan AuthRequest, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		req := <-reqCh
		if req.ID != 42 || req.Type != AuthRequestHostKey || req.Host != "example.com:2222" {
			req.Response <- AuthResponse{}
			return
		}
		req.Response <- AuthResponse{Accept: true}
	}()

	err = kh.VerifyHostKey(&AuthContext{RequestChan: reqCh, Cancel: ctx, Timeout: time.Second}, 42, "example.com", "2222", key)
	if err != nil {
		t.Fatal(err)
	}

	knownHostsPath := filepath.Join(home, ".ssh", "known_hosts")
	data, err := os.ReadFile(knownHostsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[example.com]:2222") {
		t.Fatalf("known_hosts entry = %q, want bracketed non-standard port", string(data))
	}
	if err := kh.VerifyHostKey(nil, 42, "example.com", "2222", key); err != nil {
		t.Fatalf("saved host key should verify without prompt: %v", err)
	}
}

func TestKnownHostsRejectsUnknownHost(t *testing.T) {
	kh := &KnownHosts{path: filepath.Join(t.TempDir(), "known_hosts")}
	if err := os.WriteFile(kh.path, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}
	key := testPublicKey(t)
	reqCh := make(chan AuthRequest, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		req := <-reqCh
		req.Response <- AuthResponse{Accept: false}
	}()

	err := kh.GetHostKeyCallback(&AuthContext{RequestChan: reqCh, Cancel: ctx, Timeout: time.Second}, 7)("reject.example:22", nil, key)
	if err == nil || !strings.Contains(err.Error(), "host key rejected") {
		t.Fatalf("reject error = %v, want host key rejected", err)
	}
}

func TestKnownHostsRequiresAuthContextForUnknownHost(t *testing.T) {
	kh := &KnownHosts{path: filepath.Join(t.TempDir(), "known_hosts")}
	key := testPublicKey(t)

	err := kh.VerifyHostKey(nil, 1, "missing.example", "", key)
	if err == nil || !strings.Contains(err.Error(), "HOST_KEY_UNKNOWN") {
		t.Fatalf("VerifyHostKey error = %v, want HOST_KEY_UNKNOWN", err)
	}
	if err := kh.Add("", key); err == nil {
		t.Fatal("Add should reject empty hostnames")
	}
	if err := kh.Add("unknown", key); err == nil {
		t.Fatal("Add should reject unknown hostnames")
	}
}

func TestExpandPathAndLoadPrivateKeyErrors(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("home unavailable: %v", err)
	}
	if got := expandPath("~/identity"); got != filepath.Join(home, "identity") {
		t.Fatalf("expandPath = %q, want home identity", got)
	}
	if got := expandPath("/tmp/identity"); got != "/tmp/identity" {
		t.Fatalf("absolute expandPath = %q", got)
	}

	exec := &SSHExecutor{}
	if _, err := exec.loadPrivateKey(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("loadPrivateKey should reject missing file")
	}
	invalid := filepath.Join(t.TempDir(), "bad-key")
	if err := os.WriteFile(invalid, []byte("not a key"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.loadPrivateKey(invalid); err == nil {
		t.Fatal("loadPrivateKey should reject invalid key content")
	}
}

func TestGetAuthMethodsRequiresAvailableMethod(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	exec := &SSHExecutor{}
	if _, err := exec.getAuthMethods("", nil, 1, "user", "host"); err == nil || !strings.Contains(err.Error(), "no auth methods") {
		t.Fatalf("getAuthMethods without keys/auth context error = %v, want no auth methods", err)
	}

	methods, err := exec.getAuthMethods("", &AuthContext{
		RequestChan: make(chan AuthRequest),
		Cancel:      context.Background(),
		Timeout:     time.Second,
	}, 1, "user", "host")
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) != 2 {
		t.Fatalf("auth method count = %d, want password and keyboard-interactive", len(methods))
	}
}

func TestPromptPasswordCancellationAndResponse(t *testing.T) {
	exec := &SSHExecutor{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelReqCh := make(chan AuthRequest)
	_, err := exec.promptPassword(&AuthContext{RequestChan: cancelReqCh, Cancel: ctx, Timeout: time.Millisecond}, 7, "user", "host")
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("promptPassword cancelled error = %v", err)
	}

	reqCh := make(chan AuthRequest, 1)
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	go func() {
		req := <-reqCh
		if req.ID != 8 || req.Host != "user@host" || req.Type != AuthRequestPassword {
			req.Response <- AuthResponse{}
			return
		}
		req.Response <- AuthResponse{Accept: true, Password: "secret"}
	}()
	password, err := exec.promptPassword(&AuthContext{RequestChan: reqCh, Cancel: ctx, Timeout: time.Second}, 8, "user", "host")
	if err != nil {
		t.Fatal(err)
	}
	if password != "secret" {
		t.Fatalf("password = %q, want secret", password)
	}
}

func TestKeyboardInteractiveUsesPasswordForAllQuestions(t *testing.T) {
	exec := &SSHExecutor{}
	reqCh := make(chan AuthRequest, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		req := <-reqCh
		req.Response <- AuthResponse{Accept: true, Password: "answer"}
	}()

	answers, err := exec.handleKeyboardInteractive(&AuthContext{RequestChan: reqCh, Cancel: ctx, Timeout: time.Second}, 1, "user", "host", []string{"one", "two"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal([]byte(strings.Join(answers, ",")), []byte("answer,answer")) {
		t.Fatalf("answers = %#v, want repeated password", answers)
	}
}

type countingCloser struct {
	closed int
}

func (c *countingCloser) Close() error {
	c.closed++
	return nil
}

func testPublicKey(t *testing.T) ssh.PublicKey {
	t.Helper()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return signer.PublicKey()
}

func waitForConnectionError(t *testing.T, conn Connection, want string) {
	t.Helper()

	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			t.Fatalf("connection error = %q, want substring %q", conn.Error(), want)
		case <-ticker.C:
			if strings.Contains(conn.Error(), want) {
				return
			}
		}
	}
}
