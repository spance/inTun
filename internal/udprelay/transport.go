package udprelay

import (
	"errors"
	"io"
	"net"
	"sync"
)

type FrameTransport interface {
	ReadFrame() (Frame, error)
	WriteFrame(Frame) error
	Close() error
}

func isClosedError(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, net.ErrClosed)
}

type StreamTransport struct {
	reader    io.Reader
	writer    *Writer
	closers   []io.Closer
	closeOnce sync.Once
	closeErr  error
}

func NewStreamTransport(reader io.Reader, writer io.Writer, closers ...io.Closer) *StreamTransport {
	return &StreamTransport{
		reader:  reader,
		writer:  NewWriter(writer),
		closers: closers,
	}
}

func (t *StreamTransport) ReadFrame() (Frame, error) {
	return ReadFrame(t.reader)
}

func (t *StreamTransport) WriteFrame(frame Frame) error {
	return t.writer.Write(frame)
}

func (t *StreamTransport) Close() error {
	t.closeOnce.Do(func() {
		for _, closer := range t.closers {
			if closer == nil {
				continue
			}
			if err := closer.Close(); err != nil && t.closeErr == nil {
				t.closeErr = err
			}
		}
	})
	return t.closeErr
}

func streamClosers(input io.Reader, output io.Writer) []io.Closer {
	var closers []io.Closer
	if closer, ok := input.(io.Closer); ok {
		closers = append(closers, closer)
	}
	if closer, ok := output.(io.Closer); ok {
		closers = append(closers, closer)
	}
	return closers
}
