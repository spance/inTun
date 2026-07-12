package platform

import (
	"strings"
	"testing"
)

func TestNetworkProtocolString(t *testing.T) {
	if TCP.String() != "TCP" || UDP.String() != "UDP" || NetworkProtocol(99).String() != "Unknown" {
		t.Fatal("NetworkProtocol.String returned an unexpected label")
	}
}

func TestForwardSpecValidate(t *testing.T) {
	tests := []struct {
		name    string
		spec    ForwardSpec
		wantErr string
	}{
		{name: "local TCP", spec: ForwardSpec{Type: Local, Protocol: TCP, LocalAddr: "127.0.0.1:8080", RemoteAddr: "127.0.0.1:80"}},
		{name: "local UDP", spec: ForwardSpec{Type: Local, Protocol: UDP, LocalAddr: "127.0.0.1:5353", RemoteAddr: "127.0.0.1:53"}},
		{name: "dynamic TCP", spec: ForwardSpec{Type: Dynamic, Protocol: TCP, LocalAddr: "127.0.0.1:1080"}},
		{name: "missing local", spec: ForwardSpec{Type: Local, Protocol: TCP, RemoteAddr: "127.0.0.1:80"}, wantErr: "local address"},
		{name: "missing remote", spec: ForwardSpec{Type: Local, Protocol: TCP, LocalAddr: "127.0.0.1:8080"}, wantErr: "remote address"},
		{name: "remote UDP", spec: ForwardSpec{Type: Remote, Protocol: UDP, LocalAddr: "127.0.0.1:53", RemoteAddr: "127.0.0.1:5353"}},
		{name: "dynamic UDP", spec: ForwardSpec{Type: Dynamic, Protocol: UDP, LocalAddr: "127.0.0.1:1080"}, wantErr: "not supported"},
		{name: "unknown type", spec: ForwardSpec{Type: TunnelType(99), Protocol: TCP, LocalAddr: "1", RemoteAddr: "2"}, wantErr: "unknown tunnel type"},
		{name: "unknown protocol", spec: ForwardSpec{Type: Local, Protocol: NetworkProtocol(99), LocalAddr: "1", RemoteAddr: "2"}, wantErr: "unknown network protocol"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.spec.Validate()
			if tt.wantErr == "" && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("Validate() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestForwarderSelection(t *testing.T) {
	executor := &SSHExecutor{}
	forwarder, err := executor.forwarderFor(ForwardSpec{Type: Local, Protocol: TCP})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := forwarder.(tcpForwarder); !ok {
		t.Fatalf("TCP forwarder = %T, want tcpForwarder", forwarder)
	}
	forwarder, err = executor.forwarderFor(ForwardSpec{Type: Local, Protocol: UDP})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := forwarder.(udpForwarder); !ok {
		t.Fatalf("UDP forwarder = %T, want udpForwarder", forwarder)
	}
	if _, err := executor.forwarderFor(ForwardSpec{Type: Remote, Protocol: UDP}); err != nil {
		t.Fatalf("remote UDP should select a forwarder: %v", err)
	}
}

func TestBoundedBufferRetainsPrefixAndDiscardsOverflow(t *testing.T) {
	buffer := newBoundedBuffer(5)
	if n, err := buffer.Write([]byte("abcdef")); err != nil || n != 6 {
		t.Fatalf("Write() = %d/%v, want 6/nil", n, err)
	}
	if n, err := buffer.Write([]byte("more")); err != nil || n != 4 {
		t.Fatalf("overflow Write() = %d/%v, want 4/nil", n, err)
	}
	if got := buffer.String(); got != "abcde" {
		t.Fatalf("buffer = %q, want abcde", got)
	}
}
