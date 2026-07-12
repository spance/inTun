package udprelay

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net"
	"testing"
	"time"
)

type frameReadResult struct {
	frame Frame
	err   error
}

func TestServeRelaysDatagramsAndPreservesAssociation(t *testing.T) {
	target := startUDPEchoServer(t)
	serverInput, clientOutput := io.Pipe()
	clientInput, serverOutput := io.Pipe()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- ServeTargetStream(context.Background(), serverInput, serverOutput, target, RelayOptions{
			IdleTimeout:     time.Minute,
			SweepInterval:   time.Second,
			MaxAssociations: 8,
		})
	}()

	ready, err := ReadFrame(clientInput)
	if err != nil {
		t.Fatal(err)
	}
	if ready.Type != FrameReady {
		t.Fatalf("startup frame = %d, want ready", ready.Type)
	}
	writer := NewWriter(clientOutput)
	for _, payload := range [][]byte{[]byte("first"), []byte("second")} {
		if err := writer.Write(Frame{Type: FrameData, AssociationID: 17, Payload: payload}); err != nil {
			t.Fatal(err)
		}
		response, err := ReadFrame(clientInput)
		if err != nil {
			t.Fatal(err)
		}
		if response.Type != FrameData || response.AssociationID != 17 || !bytes.Equal(response.Payload, payload) {
			t.Fatalf("response = %#v, want association 17 payload %q", response, payload)
		}
	}
	if err := clientOutput.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("relay server did not stop after input closed")
	}
	_ = clientInput.Close()
}

func TestRunCommandValidatesAndDecodesTarget(t *testing.T) {
	if err := RunCommand(context.Background(), nil, bytes.NewReader(nil), io.Discard, io.Discard); err == nil {
		t.Fatal("missing udp subcommand should fail")
	}
	if err := RunCommand(context.Background(), []string{"udp", "target", "--address-token", "%%%"}, bytes.NewReader(nil), io.Discard, io.Discard); err == nil {
		t.Fatal("invalid target token should fail")
	}
	token := base64.RawURLEncoding.EncodeToString([]byte("not a UDP address"))
	if err := RunCommand(context.Background(), []string{"udp", "target", "--address-token", token}, bytes.NewReader(nil), io.Discard, io.Discard); err == nil {
		t.Fatal("invalid decoded target should fail")
	}
	if err := RunCommand(context.Background(), []string{"udp", "listen"}, bytes.NewReader(nil), io.Discard, io.Discard); err == nil {
		t.Fatal("listen mode without an address token should fail")
	}
}

func TestRunListenCommandReportsBindFailure(t *testing.T) {
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	token := base64.RawURLEncoding.EncodeToString([]byte(listener.LocalAddr().String()))
	err = RunCommand(context.Background(), []string{"udp", "listen", "--address-token", token}, bytes.NewReader(nil), io.Discard, io.Discard)
	if err == nil {
		t.Fatal("listen mode should fail when the UDP address is already in use")
	}
}

func TestServeExpiresIdleAssociation(t *testing.T) {
	target := startUDPEchoServer(t)
	serverInput, clientOutput := io.Pipe()
	clientInput, serverOutput := io.Pipe()
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- ServeTargetStream(context.Background(), serverInput, serverOutput, target, RelayOptions{
			IdleTimeout:     20 * time.Millisecond,
			SweepInterval:   5 * time.Millisecond,
			MaxAssociations: 1,
		})
	}()
	if frame, err := ReadFrame(clientInput); err != nil || frame.Type != FrameReady {
		t.Fatalf("ready frame = %#v/%v", frame, err)
	}
	if err := WriteFrame(clientOutput, Frame{Type: FrameData, AssociationID: 1, Payload: []byte("ping")}); err != nil {
		t.Fatal(err)
	}
	if frame, err := ReadFrame(clientInput); err != nil || frame.Type != FrameData {
		t.Fatalf("data frame = %#v/%v", frame, err)
	}
	result := make(chan frameReadResult, 1)
	go func() {
		frame, err := ReadFrame(clientInput)
		result <- frameReadResult{frame: frame, err: err}
	}()
	select {
	case got := <-result:
		if got.err != nil || got.frame.Type != FrameClose || got.frame.AssociationID != 1 {
			t.Fatalf("expiry frame = %#v/%v", got.frame, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("idle association was not expired")
	}
	_ = clientOutput.Close()
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("relay server did not stop")
	}
	_ = clientInput.Close()
}

func TestServeStopsWhenContextIsCanceled(t *testing.T) {
	target := startUDPEchoServer(t)
	serverInput, clientOutput := io.Pipe()
	clientInput, serverOutput := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- ServeTargetStream(ctx, serverInput, serverOutput, target, RelayOptions{})
	}()
	if frame, err := ReadFrame(clientInput); err != nil || frame.Type != FrameReady {
		t.Fatalf("ready frame = %#v/%v", frame, err)
	}
	cancel()
	select {
	case err := <-serverDone:
		if err != context.Canceled {
			t.Fatalf("Serve() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("relay server did not stop after context cancellation")
	}
	_ = clientOutput.Close()
	_ = clientInput.Close()
}

func startUDPEchoServer(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, MaxDatagramSize)
		for {
			n, peer, err := conn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			_, _ = conn.WriteToUDP(buffer[:n], peer)
		}
	}()
	t.Cleanup(func() {
		_ = conn.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("UDP echo server did not stop")
		}
	})
	return conn.LocalAddr().String()
}
