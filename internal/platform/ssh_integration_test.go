package platform

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	pkgsftp "github.com/pkg/sftp"
)

func TestSSHExecutorConnectsAndCreatesSFTPClientWithInProcessServer(t *testing.T) {
	remoteRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(remoteRoot, "remote.txt"), []byte("remote-data"), 0644); err != nil {
		t.Fatal(err)
	}
	server := startTestSSHServer(t, remoteRoot)

	t.Setenv("HOME", t.TempDir())
	authCtx := newIntegrationAuthContext(t)
	connIface, err := (&SSHExecutor{}).Connect(authCtx, server.config(), ForwardSpec{Type: Dynamic, Protocol: TCP, LocalAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	conn := connIface.(*SSHConnection)
	t.Cleanup(func() { _ = conn.Stop() })
	waitForSSHClientReady(t, conn)

	rawClient, err := conn.NewSFTPClient()
	if err != nil {
		t.Fatal(err)
	}
	client := rawClient.(*pkgsftp.Client)
	defer client.Close()

	entries, err := client.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "remote.txt" {
		t.Fatalf("remote entries = %#v, want remote.txt", entries)
	}
	if latency := conn.Ping(); latency <= 0 {
		t.Fatalf("Ping latency = %v, want positive keepalive round-trip", latency)
	}
}

func TestSSHExecutorLocalForwardWithInProcessServer(t *testing.T) {
	echoAddr := startEchoServer(t)
	server := startTestSSHServer(t, t.TempDir())

	t.Setenv("HOME", t.TempDir())
	authCtx := newIntegrationAuthContext(t)
	connIface, err := (&SSHExecutor{}).Connect(authCtx, server.config(), ForwardSpec{Type: Local, Protocol: TCP, LocalAddr: "127.0.0.1:0", RemoteAddr: echoAddr})
	if err != nil {
		t.Fatal(err)
	}
	conn := connIface.(*SSHConnection)
	t.Cleanup(func() { _ = conn.Stop() })
	listener := waitForForwardListener(t, conn)

	roundTripTCP(t, listener.Addr().String(), "local-forward")
	waitForNonZeroStats(t, conn)
}

func TestSSHExecutorLocalUDPForwardWithInProcessServer(t *testing.T) {
	echoAddr := startUDPEchoServer(t)
	server := startTestSSHServer(t, t.TempDir())

	t.Setenv("HOME", t.TempDir())
	authCtx := newIntegrationAuthContext(t)
	connIface, err := (&SSHExecutor{}).Connect(authCtx, server.config(), ForwardSpec{
		Type:       Local,
		Protocol:   UDP,
		LocalAddr:  "127.0.0.1:0",
		RemoteAddr: echoAddr,
	})
	if err != nil {
		t.Fatal(err)
	}
	conn := connIface.(*SSHConnection)
	t.Cleanup(func() { _ = conn.Stop() })
	forward := waitForUDPForwardRuntime(t, conn)

	for _, message := range []string{"first-datagram", "second-datagram"} {
		client, err := net.Dial("udp", forward.listener.LocalAddr().String())
		if err != nil {
			t.Fatal(err)
		}
		if err := client.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
			_ = client.Close()
			t.Fatal(err)
		}
		if _, err := client.Write([]byte(message)); err != nil {
			_ = client.Close()
			t.Fatal(err)
		}
		buffer := make([]byte, 128)
		n, err := client.Read(buffer)
		_ = client.Close()
		if err != nil {
			t.Fatal(err)
		}
		if string(buffer[:n]) != message {
			t.Fatalf("UDP round-trip = %q, want %q", buffer[:n], message)
		}
	}
	waitForNonZeroStats(t, conn)
}

func TestSSHExecutorRemoteUDPForwardWithInProcessServer(t *testing.T) {
	echoAddr := startUDPEchoServer(t)
	server := startTestSSHServer(t, t.TempDir())

	t.Setenv("HOME", t.TempDir())
	authCtx := newIntegrationAuthContext(t)
	connIface, err := (&SSHExecutor{}).Connect(authCtx, server.config(), ForwardSpec{
		Type:       Remote,
		Protocol:   UDP,
		LocalAddr:  echoAddr,
		RemoteAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatal(err)
	}
	conn := connIface.(*SSHConnection)
	forward := waitForUDPForwardRuntime(t, conn)
	if forward.listener != nil {
		t.Fatal("remote UDP forward should not own a local peer listener")
	}
	if forward.remoteAddr == "" || strings.HasSuffix(forward.remoteAddr, ":0") {
		t.Fatalf("remote UDP bound address = %q, want allocated port", forward.remoteAddr)
	}

	for _, message := range []string{"remote-first", "remote-second"} {
		client, err := net.Dial("udp", forward.remoteAddr)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
			_ = client.Close()
			t.Fatal(err)
		}
		if _, err := client.Write([]byte(message)); err != nil {
			_ = client.Close()
			t.Fatal(err)
		}
		buffer := make([]byte, 128)
		n, err := client.Read(buffer)
		_ = client.Close()
		if err != nil {
			t.Fatal(err)
		}
		if string(buffer[:n]) != message {
			t.Fatalf("remote UDP round-trip = %q, want %q", buffer[:n], message)
		}
	}
	waitForNonZeroStats(t, conn)
	if err := conn.Stop(); err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	waitForCondition(t, func() bool {
		addr, err := net.ResolveUDPAddr("udp", forward.remoteAddr)
		if err != nil {
			t.Fatal(err)
		}
		listener, err := net.ListenUDP("udp", addr)
		if err != nil {
			return false
		}
		_ = listener.Close()
		return true
	})
}

func TestSSHExecutorRemoteUDPBindFailureDoesNotBecomeRunning(t *testing.T) {
	occupied, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	server := startTestSSHServer(t, t.TempDir())

	t.Setenv("HOME", t.TempDir())
	connIface, err := (&SSHExecutor{}).Connect(newIntegrationAuthContext(t), server.config(), ForwardSpec{
		Type:       Remote,
		Protocol:   UDP,
		LocalAddr:  "127.0.0.1:53",
		RemoteAddr: occupied.LocalAddr().String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	conn := connIface.(*SSHConnection)
	t.Cleanup(func() { _ = conn.Stop() })
	waitForConnectionError(t, conn, "UDP_RELAY_FAILED")
	if conn.IsRunning() {
		t.Fatal("remote UDP bind failure must not become Running")
	}
}

func TestSSHExecutorRemoteForwardWithInProcessServer(t *testing.T) {
	echoAddr := startEchoServer(t)
	server := startTestSSHServer(t, t.TempDir())

	t.Setenv("HOME", t.TempDir())
	authCtx := newIntegrationAuthContext(t)
	connIface, err := (&SSHExecutor{}).Connect(authCtx, server.config(), ForwardSpec{Type: Remote, Protocol: TCP, LocalAddr: echoAddr, RemoteAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	conn := connIface.(*SSHConnection)
	t.Cleanup(func() { _ = conn.Stop() })
	listener := waitForForwardListener(t, conn)

	roundTripTCP(t, listener.Addr().String(), "remote-forward")
	waitForNonZeroStats(t, conn)
}

func TestSSHExecutorDynamicForwardWithInProcessServer(t *testing.T) {
	echoAddr := startEchoServer(t)
	host, portStr, err := net.SplitHostPort(echoAddr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	server := startTestSSHServer(t, t.TempDir())

	t.Setenv("HOME", t.TempDir())
	authCtx := newIntegrationAuthContext(t)
	connIface, err := (&SSHExecutor{}).Connect(authCtx, server.config(), ForwardSpec{Type: Dynamic, Protocol: TCP, LocalAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	conn := connIface.(*SSHConnection)
	t.Cleanup(func() { _ = conn.Stop() })
	listener := waitForForwardListener(t, conn)

	socksConn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer socksConn.Close()
	if err := socksConn.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := socksConn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatal(err)
	}
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(socksConn, greeting); err != nil {
		t.Fatal(err)
	}
	if greeting[0] != 0x05 || greeting[1] != 0x00 {
		t.Fatalf("SOCKS greeting = %#v, want no-auth success", greeting)
	}

	ip := net.ParseIP(host).To4()
	if ip == nil {
		t.Fatalf("echo host %q is not IPv4", host)
	}
	request := []byte{0x05, 0x01, 0x00, 0x01, ip[0], ip[1], ip[2], ip[3], byte(port >> 8), byte(port)}
	if _, err := socksConn.Write(request); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 10)
	if _, err := io.ReadFull(socksConn, reply); err != nil {
		t.Fatal(err)
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		t.Fatalf("SOCKS connect reply = %#v, want success", reply)
	}
	if _, err := socksConn.Write([]byte("dynamic-forward")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len("dynamic-forward"))
	if _, err := io.ReadFull(socksConn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "dynamic-forward" {
		t.Fatalf("SOCKS round-trip = %q, want dynamic-forward", string(buf))
	}
	waitForNonZeroStats(t, conn)
}

func waitForNonZeroStats(t *testing.T, conn *SSHConnection) {
	t.Helper()
	waitForCondition(t, func() bool {
		upload, download := conn.GetStats()
		return upload > 0 && download > 0
	})
}
