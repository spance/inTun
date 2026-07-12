package udprelay

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
)

const (
	protocolMagic   = 0x4955
	ProtocolVersion = 1
	MaxDatagramSize = 65507
	frameHeaderSize = 12
)

// Frames use a fixed header: magic(2), version(1), type(1), association(4), payload length(4).

type FrameType uint8

const (
	FrameReady FrameType = iota + 1
	FrameData
	FrameClose
	FrameError
)

type Frame struct {
	Type          FrameType
	AssociationID uint32
	Payload       []byte
}

var (
	ErrInvalidFrame    = errors.New("invalid UDP relay frame")
	ErrFrameTooLarge   = errors.New("UDP relay frame exceeds maximum datagram size")
	ErrProtocolVersion = errors.New("unsupported UDP relay protocol version")
)

func ReadFrame(r io.Reader) (Frame, error) {
	var header [frameHeaderSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Frame{}, err
	}
	if binary.BigEndian.Uint16(header[0:2]) != protocolMagic {
		return Frame{}, fmt.Errorf("%w: bad magic", ErrInvalidFrame)
	}
	if header[2] != ProtocolVersion {
		return Frame{}, fmt.Errorf("%w: %d", ErrProtocolVersion, header[2])
	}
	frameType := FrameType(header[3])
	if frameType < FrameReady || frameType > FrameError {
		return Frame{}, fmt.Errorf("%w: unknown type %d", ErrInvalidFrame, frameType)
	}
	payloadSize := binary.BigEndian.Uint32(header[8:12])
	if payloadSize > MaxDatagramSize {
		return Frame{}, ErrFrameTooLarge
	}
	payload := make([]byte, int(payloadSize))
	if _, err := io.ReadFull(r, payload); err != nil {
		return Frame{}, err
	}
	return Frame{
		Type:          frameType,
		AssociationID: binary.BigEndian.Uint32(header[4:8]),
		Payload:       payload,
	}, nil
}

func WriteFrame(w io.Writer, frame Frame) error {
	if frame.Type < FrameReady || frame.Type > FrameError {
		return fmt.Errorf("%w: unknown type %d", ErrInvalidFrame, frame.Type)
	}
	if len(frame.Payload) > MaxDatagramSize {
		return ErrFrameTooLarge
	}
	var header [frameHeaderSize]byte
	binary.BigEndian.PutUint16(header[0:2], protocolMagic)
	header[2] = ProtocolVersion
	header[3] = byte(frame.Type)
	binary.BigEndian.PutUint32(header[4:8], frame.AssociationID)
	binary.BigEndian.PutUint32(header[8:12], uint32(len(frame.Payload)))
	if err := writeAll(w, header[:]); err != nil {
		return err
	}
	return writeAll(w, frame.Payload)
}

type Writer struct {
	w  io.Writer
	mu sync.Mutex
}

func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

func (w *Writer) Write(frame Frame) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return WriteFrame(w.w, frame)
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
