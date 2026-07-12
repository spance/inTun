package platform

import (
	"sync/atomic"

	"github.com/spance/intun/internal/udprelay"
)

type countingFrameTransport struct {
	inner    udprelay.FrameTransport
	upload   *atomic.Int64
	download *atomic.Int64
}

func newCountingFrameTransport(inner udprelay.FrameTransport, upload, download *atomic.Int64) *countingFrameTransport {
	return &countingFrameTransport{inner: inner, upload: upload, download: download}
}

func (t *countingFrameTransport) ReadFrame() (udprelay.Frame, error) {
	frame, err := t.inner.ReadFrame()
	if err == nil && frame.Type == udprelay.FrameData {
		t.download.Add(int64(len(frame.Payload)))
	}
	return frame, err
}

func (t *countingFrameTransport) WriteFrame(frame udprelay.Frame) error {
	if err := t.inner.WriteFrame(frame); err != nil {
		return err
	}
	if frame.Type == udprelay.FrameData {
		t.upload.Add(int64(len(frame.Payload)))
	}
	return nil
}

func (t *countingFrameTransport) Close() error {
	return t.inner.Close()
}
