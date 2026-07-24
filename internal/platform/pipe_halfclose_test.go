package platform

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestProxyTCPPreservesHalfCloseResponse(t *testing.T) {
	leftProxy, leftPeer := tcpConnectionPair(t)
	rightProxy, rightPeer := tcpConnectionPair(t)
	defer leftPeer.Close()
	defer rightPeer.Close()

	done := make(chan struct{})
	go func() {
		proxyTCP(leftProxy, rightProxy)
		close(done)
	}()

	if _, err := leftPeer.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	if err := leftPeer.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	request, err := io.ReadAll(rightPeer)
	if err != nil {
		t.Fatal(err)
	}
	if string(request) != "request" {
		t.Fatalf("request = %q", request)
	}
	if _, err := rightPeer.Write([]byte("response")); err != nil {
		t.Fatal(err)
	}
	if err := rightPeer.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	response, err := io.ReadAll(leftPeer)
	if err != nil {
		t.Fatal(err)
	}
	if string(response) != "response" {
		t.Fatalf("response = %q", response)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("proxy did not exit after both half-closes")
	}
}

func tcpConnectionPair(t *testing.T) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan *net.TCPConn, 1)
	errs := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.AcceptTCP()
		if acceptErr != nil {
			errs <- acceptErr
			return
		}
		accepted <- conn
	}()
	peer, err := net.DialTCP("tcp", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case conn := <-accepted:
		_ = listener.Close()
		return conn, peer
	case err := <-errs:
		_ = listener.Close()
		_ = peer.Close()
		t.Fatal(err)
		return nil, nil
	}
}
