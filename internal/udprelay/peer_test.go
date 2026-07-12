package udprelay

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestPeerAndTargetRelaysPreserveRemotePeerIsolation(t *testing.T) {
	target := startUDPEchoServer(t)
	peerConn, targetConn := net.Pipe()
	peerTransport := NewStreamTransport(peerConn, peerConn, peerConn)
	targetTransport := NewStreamTransport(targetConn, targetConn, targetConn)
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	peerDone := make(chan error, 1)
	targetDone := make(chan error, 1)
	go func() {
		peerDone <- NewPeerRelay(listener, peerTransport, RelayOptions{}, PeerHooks{}).Serve(ctx)
	}()
	go func() {
		targetDone <- ServeTarget(ctx, targetTransport, target, RelayOptions{}, TargetHooks{})
	}()

	clients := make([]net.Conn, 2)
	for i, message := range []string{"peer-one", "peer-two"} {
		client, err := net.Dial("udp", listener.LocalAddr().String())
		if err != nil {
			t.Fatal(err)
		}
		clients[i] = client
		if err := client.SetDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := client.Write([]byte(message)); err != nil {
			t.Fatal(err)
		}
		buffer := make([]byte, 64)
		n, err := client.Read(buffer)
		if err != nil {
			t.Fatal(err)
		}
		if string(buffer[:n]) != message {
			t.Fatalf("peer %d response = %q, want %q", i, buffer[:n], message)
		}
	}
	for _, client := range clients {
		_ = client.Close()
	}
	cancel()
	for name, done := range map[string]<-chan error{"peer": peerDone, "target": targetDone} {
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Fatalf("%s relay error = %v", name, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s relay did not stop", name)
		}
	}
}
