package platform

import "testing"

func TestTCPForwardAddr(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{
			name: "plain port defaults to localhost",
			addr: "5551",
			want: "127.0.0.1:5551",
		},
		{
			name: "host and port are preserved",
			addr: "10.0.0.15:5551",
			want: "10.0.0.15:5551",
		},
		{
			name: "localhost and port are preserved",
			addr: "127.0.0.1:5551",
			want: "127.0.0.1:5551",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tcpForwardAddr(tt.addr); got != tt.want {
				t.Fatalf("tcpForwardAddr(%q) = %q, want %q", tt.addr, got, tt.want)
			}
		})
	}
}

func TestSetConnectionLostDoesNotOverwriteExistingError(t *testing.T) {
	conn := &SSHConnection{}

	conn.setError("REMOTE_LISTEN_FAILED: bind: address already in use")
	conn.setConnectionLost("SSH_CONNECTION_LOST: use of closed network connection")

	if got := conn.Error(); got != "REMOTE_LISTEN_FAILED: bind: address already in use" {
		t.Fatalf("Error() = %q, want original remote listen error", got)
	}
	if conn.IsRunning() {
		t.Fatal("connection should not be running after setError")
	}
}

func TestSetConnectionLostStoresLossWhenNoEarlierError(t *testing.T) {
	conn := &SSHConnection{}

	conn.setConnectionLost("SSH_CONNECTION_LOST: EOF")

	if got := conn.Error(); got != "SSH_CONNECTION_LOST: EOF" {
		t.Fatalf("Error() = %q, want connection lost error", got)
	}
}
