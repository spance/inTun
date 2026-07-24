//go:build !windows

package platform

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh/agent"
)

func TestAgentAuthMethodUsesConfiguredSocket(t *testing.T) {
	tempDir, err := os.MkdirTemp("/tmp", "intun-agent-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })
	socketPath := filepath.Join(tempDir, "agent.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: privateKey}); err != nil {
		t.Fatal(err)
	}
	serveErr := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			serveErr <- acceptErr
			return
		}
		defer conn.Close()
		serveErr <- agent.ServeAgent(keyring, conn)
	}()

	method, closer, err := agentAuthMethod(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if method == nil || closer == nil {
		t.Fatal("agentAuthMethod should return an auth method and its connection")
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-serveErr; err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}
