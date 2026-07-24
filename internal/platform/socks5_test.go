package platform

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestNegotiateSOCKS5HandlesFragmentedDomainRequest(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	target := make(chan string, 1)
	errs := make(chan error, 1)
	go func() {
		got, err := negotiateSOCKS5(server)
		if err != nil {
			errs <- err
			return
		}
		target <- got
	}()

	writeFragments(t, client, []byte{0x05}, []byte{0x02, 0x02}, []byte{0x00})
	method := make([]byte, 2)
	if _, err := io.ReadFull(client, method); err != nil {
		t.Fatal(err)
	}
	if method[1] != socksNoAuth {
		t.Fatalf("method reply = %#v", method)
	}

	host := "example.com"
	request := append([]byte{socksVersion, socksConnect, 0, socksDomain, byte(len(host))}, []byte(host)...)
	request = append(request, 0x01, 0xbb)
	for _, value := range request {
		writeFragments(t, client, []byte{value})
	}

	select {
	case got := <-target:
		if got != "example.com:443" {
			t.Fatalf("target = %q", got)
		}
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("fragmented SOCKS5 request did not complete")
	}
}

func TestNegotiateSOCKS5RejectsMissingNoAuthMethod(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	errs := make(chan error, 1)
	go func() {
		_, err := negotiateSOCKS5(server)
		errs <- err
	}()

	writeFragments(t, client, []byte{socksVersion, 1, 2})
	reply := make([]byte, 2)
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatal(err)
	}
	if reply[1] != socksNoAcceptable {
		t.Fatalf("method reply = %#v", reply)
	}
	if err := <-errs; !errors.Is(err, errSOCKSNoAuth) {
		t.Fatalf("error = %v", err)
	}
}

func writeFragments(t *testing.T, conn net.Conn, fragments ...[]byte) {
	t.Helper()
	for _, fragment := range fragments {
		if _, err := conn.Write(fragment); err != nil {
			t.Fatal(err)
		}
	}
}

func FuzzReadSOCKSHost(f *testing.F) {
	f.Add(byte(socksIPv4), []byte{127, 0, 0, 1})
	f.Add(byte(socksIPv6), make([]byte, net.IPv6len))
	f.Add(byte(socksDomain), append([]byte{3}, []byte("dev")...))
	f.Fuzz(func(t *testing.T, addressType byte, payload []byte) {
		server, client := net.Pipe()
		deadline := time.Now().Add(50 * time.Millisecond)
		_ = server.SetDeadline(deadline)
		_ = client.SetDeadline(deadline)
		go func() {
			_, _ = client.Write(payload)
			_ = client.Close()
		}()
		_, _ = readSOCKSHost(server, addressType)
		_ = server.Close()
	})
}
