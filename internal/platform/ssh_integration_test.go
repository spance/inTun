package platform

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
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
	connIface, err := (&SSHExecutor{}).Connect(authCtx, server.config(), Dynamic, "127.0.0.1:0", "")
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
	connIface, err := (&SSHExecutor{}).Connect(authCtx, server.config(), Local, "127.0.0.1:0", echoAddr)
	if err != nil {
		t.Fatal(err)
	}
	conn := connIface.(*SSHConnection)
	t.Cleanup(func() { _ = conn.Stop() })
	listener := waitForForwardListener(t, conn)

	roundTripTCP(t, listener.Addr().String(), "local-forward")
	upload, download := conn.GetStats()
	if upload == 0 || download == 0 {
		t.Fatalf("forward stats upload/download = %d/%d, want non-zero", upload, download)
	}
}

func TestSSHExecutorRemoteForwardWithInProcessServer(t *testing.T) {
	echoAddr := startEchoServer(t)
	server := startTestSSHServer(t, t.TempDir())

	t.Setenv("HOME", t.TempDir())
	authCtx := newIntegrationAuthContext(t)
	connIface, err := (&SSHExecutor{}).Connect(authCtx, server.config(), Remote, echoAddr, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	conn := connIface.(*SSHConnection)
	t.Cleanup(func() { _ = conn.Stop() })
	listener := waitForForwardListener(t, conn)

	roundTripTCP(t, listener.Addr().String(), "remote-forward")
	upload, download := conn.GetStats()
	if upload == 0 || download == 0 {
		t.Fatalf("remote stats upload/download = %d/%d, want non-zero", upload, download)
	}
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
	connIface, err := (&SSHExecutor{}).Connect(authCtx, server.config(), Dynamic, "127.0.0.1:0", "")
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
	upload, download := conn.GetStats()
	if upload == 0 || download == 0 {
		t.Fatalf("dynamic stats upload/download = %d/%d, want non-zero", upload, download)
	}
}
