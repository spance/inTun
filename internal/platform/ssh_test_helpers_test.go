package platform

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func newIntegrationAuthContext(t *testing.T) *AuthContext {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	reqCh := make(chan AuthRequest)
	t.Cleanup(cancel)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case req := <-reqCh:
				switch req.Type {
				case AuthRequestHostKey:
					req.Response <- AuthResponse{Accept: true}
				case AuthRequestPassword:
					req.Response <- AuthResponse{Accept: true, Password: "testpass"}
				default:
					req.Response <- AuthResponse{}
				}
			}
		}
	}()

	return &AuthContext{
		RequestChan: reqCh,
		Cancel:      ctx,
		Timeout:     time.Second,
	}
}

func newIntegrationSigner(t *testing.T) ssh.Signer {
	t.Helper()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func waitForSSHClientReady(t *testing.T, conn *SSHConnection) {
	t.Helper()

	waitForCondition(t, func() bool {
		conn.mu.RLock()
		defer conn.mu.RUnlock()
		if conn.lastError != "" {
			t.Fatalf("connection failed before becoming ready: %s", conn.lastError)
		}
		return conn.client != nil
	})
}

func waitForForwardListener(t *testing.T, conn *SSHConnection) net.Listener {
	t.Helper()

	var listener net.Listener
	waitForCondition(t, func() bool {
		conn.mu.RLock()
		defer conn.mu.RUnlock()
		if conn.lastError != "" {
			t.Fatalf("connection failed before listener became ready: %s", conn.lastError)
		}
		if len(conn.forwards) == 0 {
			return false
		}
		var ok bool
		listener, ok = conn.forwards[0].(net.Listener)
		if !ok {
			t.Fatalf("forward %T is not a net.Listener", conn.forwards[0])
		}
		return true
	})
	return listener
}

func waitForCondition(t *testing.T, ready func() bool) {
	t.Helper()

	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("condition did not become ready")
		case <-ticker.C:
			if ready() {
				return
			}
		}
	}
}

func startEchoServer(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("echo server did not stop")
		}
	})
	return listener.Addr().String()
}

func roundTripTCP(t *testing.T, addr string, message string) {
	t.Helper()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte(message)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(message))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(buf)) != message {
		t.Fatalf("round-trip = %q, want %q", string(buf), message)
	}
}
