package platform

import (
	"io"
	"sync/atomic"
	"testing"

	"github.com/spance/intun/internal/udprelay"
)

func TestCountingFrameTransportCountsPayloadByLocalDirection(t *testing.T) {
	inner := &stubFrameTransport{
		reads: []udprelay.Frame{
			{Type: udprelay.FrameData, Payload: []byte("download")},
			{Type: udprelay.FrameClose},
		},
	}
	var upload atomic.Int64
	var download atomic.Int64
	transport := newCountingFrameTransport(inner, &upload, &download)

	if _, err := transport.ReadFrame(); err != nil {
		t.Fatal(err)
	}
	if _, err := transport.ReadFrame(); err != nil {
		t.Fatal(err)
	}
	if err := transport.WriteFrame(udprelay.Frame{Type: udprelay.FrameData, Payload: []byte("upload")}); err != nil {
		t.Fatal(err)
	}
	if err := transport.WriteFrame(udprelay.Frame{Type: udprelay.FrameError, Payload: []byte("ignored")}); err != nil {
		t.Fatal(err)
	}
	if upload.Load() != int64(len("upload")) || download.Load() != int64(len("download")) {
		t.Fatalf("payload stats = upload:%d download:%d", upload.Load(), download.Load())
	}
}

type stubFrameTransport struct {
	reads  []udprelay.Frame
	writes []udprelay.Frame
}

func (t *stubFrameTransport) ReadFrame() (udprelay.Frame, error) {
	if len(t.reads) == 0 {
		return udprelay.Frame{}, io.EOF
	}
	frame := t.reads[0]
	t.reads = t.reads[1:]
	return frame, nil
}

func (t *stubFrameTransport) WriteFrame(frame udprelay.Frame) error {
	t.writes = append(t.writes, frame)
	return nil
}

func (t *stubFrameTransport) Close() error {
	return nil
}
