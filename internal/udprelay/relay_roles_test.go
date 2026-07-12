package udprelay

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestTargetRelayRejectsZeroAssociation(t *testing.T) {
	transport := &scriptedFrameTransport{reads: []Frame{{Type: FrameData}}}
	relay, err := NewTargetRelay(transport, "127.0.0.1:9", RelayOptions{}, TargetHooks{})
	if err != nil {
		t.Fatal(err)
	}
	err = relay.Serve(context.Background())
	if !errors.Is(err, ErrInvalidFrame) {
		t.Fatalf("Serve() error = %v, want ErrInvalidFrame", err)
	}
}

func TestTargetRelayReportsAssociationLimit(t *testing.T) {
	target := startUDPSink(t)
	transport := &scriptedFrameTransport{
		reads: []Frame{
			{Type: FrameData, AssociationID: 1, Payload: []byte("first")},
			{Type: FrameData, AssociationID: 2, Payload: []byte("second")},
		},
	}
	var hookID uint32
	var hookErr error
	relay, err := NewTargetRelay(transport, target, RelayOptions{MaxAssociations: 1}, TargetHooks{
		OnAssociationError: func(id uint32, err error) {
			hookID = id
			hookErr = err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := relay.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hookID != 2 || !errors.Is(hookErr, ErrAssociationLimit) {
		t.Fatalf("association hook = %d/%v, want 2/ErrAssociationLimit", hookID, hookErr)
	}
	writes := transport.Writes()
	if len(writes) != 1 || writes[0].Type != FrameError || writes[0].AssociationID != 2 {
		t.Fatalf("target error frames = %#v", writes)
	}
}

func TestTargetRelayHandlesPeerClose(t *testing.T) {
	target := startUDPSink(t)
	transport := &scriptedFrameTransport{
		reads: []Frame{
			{Type: FrameData, AssociationID: 7, Payload: []byte("open")},
			{Type: FrameClose, AssociationID: 7},
		},
	}
	relay, err := NewTargetRelay(transport, target, RelayOptions{}, TargetHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if err := relay.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(relay.sessions) != 0 {
		t.Fatalf("target associations after close = %d, want 0", len(relay.sessions))
	}
}

func TestPeerRelayExpiresAndHandlesControlFrames(t *testing.T) {
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	transport := &scriptedFrameTransport{}
	var hookID uint32
	relay := NewPeerRelay(listener, transport, RelayOptions{IdleTimeout: time.Minute}, PeerHooks{
		OnAssociationError: func(id uint32, message string) {
			hookID = id
		},
	})
	old := time.Now().Add(-2 * time.Minute)
	firstID, err := relay.registry.Resolve(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1001}, old)
	if err != nil {
		t.Fatal(err)
	}
	if err := relay.expireAssociations(time.Now()); err != nil {
		t.Fatal(err)
	}
	if writes := transport.Writes(); len(writes) != 1 || writes[0].Type != FrameClose || writes[0].AssociationID != firstID {
		t.Fatalf("peer expiry frames = %#v", writes)
	}
	secondID, err := relay.registry.Resolve(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1002}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	transport.SetReads([]Frame{
		{Type: FrameError, AssociationID: secondID, Payload: []byte("target failed")},
		{Type: FrameReady},
	})
	err = relay.readFrames(context.Background())
	if !errors.Is(err, ErrInvalidFrame) || hookID != secondID || relay.registry.Len() != 0 {
		t.Fatalf("peer control result = err:%v hook:%d associations:%d", err, hookID, relay.registry.Len())
	}
	_ = relay.Close()
}

func TestStreamTransportCloseIsIdempotent(t *testing.T) {
	wantErr := errors.New("close failed")
	first := &testCloser{}
	second := &testCloser{err: wantErr}
	transport := NewStreamTransport(&scriptedFrameTransport{}, io.Discard, first, nil, second)
	if err := transport.Close(); !errors.Is(err, wantErr) {
		t.Fatalf("Close() error = %v, want close failed", err)
	}
	if err := transport.Close(); !errors.Is(err, wantErr) {
		t.Fatalf("second Close() error = %v, want preserved close failed", err)
	}
	if first.count != 1 || second.count != 1 {
		t.Fatalf("closer counts = %d/%d, want 1/1", first.count, second.count)
	}
}

type scriptedFrameTransport struct {
	mu     sync.Mutex
	reads  []Frame
	writes []Frame
	closed bool
}

func (t *scriptedFrameTransport) Read(_ []byte) (int, error) {
	return 0, io.EOF
}

func (t *scriptedFrameTransport) ReadFrame() (Frame, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.reads) == 0 {
		return Frame{}, io.EOF
	}
	frame := t.reads[0]
	t.reads = t.reads[1:]
	return frame, nil
}

func (t *scriptedFrameTransport) WriteFrame(frame Frame) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return io.ErrClosedPipe
	}
	t.writes = append(t.writes, frame)
	return nil
}

func (t *scriptedFrameTransport) Close() error {
	t.mu.Lock()
	t.closed = true
	t.mu.Unlock()
	return nil
}

func (t *scriptedFrameTransport) Writes() []Frame {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]Frame(nil), t.writes...)
}

func (t *scriptedFrameTransport) SetReads(frames []Frame) {
	t.mu.Lock()
	t.reads = append([]Frame(nil), frames...)
	t.closed = false
	t.mu.Unlock()
}

type testCloser struct {
	err   error
	count int
}

func (c *testCloser) Close() error {
	c.count++
	return c.err
}

func startUDPSink(t *testing.T) string {
	t.Helper()
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, MaxDatagramSize)
		for {
			if _, _, err := listener.ReadFromUDP(buffer); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("UDP sink did not stop")
		}
	})
	return listener.LocalAddr().String()
}
