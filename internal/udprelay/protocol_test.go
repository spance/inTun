package udprelay

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	want := Frame{Type: FrameData, AssociationID: 42, Payload: []byte("datagram")}
	var encoded bytes.Buffer
	if err := WriteFrame(&encoded, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFrame(&encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != want.Type || got.AssociationID != want.AssociationID || !bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("ReadFrame() = %#v, want %#v", got, want)
	}
}

func TestWriteFrameHandlesShortWrites(t *testing.T) {
	writer := &chunkWriter{max: 3}
	want := Frame{Type: FrameData, AssociationID: 7, Payload: []byte("payload")}
	if err := WriteFrame(writer, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFrame(bytes.NewReader(writer.data))
	if err != nil {
		t.Fatal(err)
	}
	if got.AssociationID != want.AssociationID || !bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("short-write round trip = %#v, want %#v", got, want)
	}
}

func TestReadFrameRejectsMalformedHeaders(t *testing.T) {
	validHeader := make([]byte, frameHeaderSize)
	binary.BigEndian.PutUint16(validHeader[0:2], protocolMagic)
	validHeader[2] = ProtocolVersion
	validHeader[3] = byte(FrameData)

	tests := []struct {
		name string
		edit func([]byte)
		want error
	}{
		{name: "magic", edit: func(header []byte) { header[0] = 0 }, want: ErrInvalidFrame},
		{name: "version", edit: func(header []byte) { header[2]++ }, want: ErrProtocolVersion},
		{name: "type", edit: func(header []byte) { header[3] = 99 }, want: ErrInvalidFrame},
		{name: "size", edit: func(header []byte) { binary.BigEndian.PutUint32(header[8:12], MaxDatagramSize+1) }, want: ErrFrameTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := append([]byte(nil), validHeader...)
			tt.edit(header)
			_, err := ReadFrame(bytes.NewReader(header))
			if !errors.Is(err, tt.want) {
				t.Fatalf("ReadFrame() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestWriteFrameRejectsOversizedDatagram(t *testing.T) {
	err := WriteFrame(io.Discard, Frame{Type: FrameData, Payload: make([]byte, MaxDatagramSize+1)})
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("WriteFrame() error = %v, want ErrFrameTooLarge", err)
	}
}

func FuzzFrameRoundTrip(f *testing.F) {
	f.Add(uint32(1), []byte("hello"))
	f.Add(uint32(99), []byte{})
	f.Fuzz(func(t *testing.T, associationID uint32, payload []byte) {
		if len(payload) > MaxDatagramSize {
			t.Skip()
		}
		want := Frame{Type: FrameData, AssociationID: associationID, Payload: payload}
		var encoded bytes.Buffer
		if err := WriteFrame(&encoded, want); err != nil {
			t.Fatal(err)
		}
		got, err := ReadFrame(&encoded)
		if err != nil {
			t.Fatal(err)
		}
		if got.Type != want.Type || got.AssociationID != want.AssociationID || !bytes.Equal(got.Payload, payload) {
			t.Fatalf("round trip = %#v, want %#v", got, want)
		}
	})
}

type chunkWriter struct {
	max  int
	data []byte
}

func (w *chunkWriter) Write(data []byte) (int, error) {
	if len(data) > w.max {
		data = data[:w.max]
	}
	w.data = append(w.data, data...)
	return len(data), nil
}
