package platform

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	pkgsftp "github.com/pkg/sftp"
	"github.com/spance/intun/internal/udprelay"
	"golang.org/x/crypto/ssh"
)

type testSSHServer struct {
	listener net.Listener
	root     string
	done     chan struct{}
}

func startTestSSHServer(t *testing.T, root string) *testSSHServer {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &testSSHServer{
		listener: listener,
		root:     root,
		done:     make(chan struct{}),
	}
	config := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if conn.User() == "testuser" && string(password) == "testpass" {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected for %s", conn.User())
		},
	}
	config.AddHostKey(newIntegrationSigner(t))

	go server.serve(config)
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case <-server.done:
		case <-time.After(time.Second):
			t.Fatalf("test SSH server did not stop")
		}
	})
	return server
}

func (s *testSSHServer) config() *SSHConfig {
	host, port, err := net.SplitHostPort(s.listener.Addr().String())
	if err != nil {
		panic(err)
	}
	return &SSHConfig{
		Host: host,
		Port: port,
		User: "testuser",
	}
}

func (s *testSSHServer) serve(config *ssh.ServerConfig) {
	defer close(s.done)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn, config)
	}
}

func (s *testSSHServer) handleConn(conn net.Conn, config *ssh.ServerConfig) {
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		_ = conn.Close()
		return
	}
	go func() {
		_ = sshConn.Wait()
	}()
	go handleGlobalRequests(sshConn, reqs)
	for newChan := range chans {
		switch newChan.ChannelType() {
		case "session":
			go s.handleSession(newChan)
		case "direct-tcpip":
			go handleDirectTCPIP(newChan)
		default:
			_ = newChan.Reject(ssh.UnknownChannelType, "unsupported channel")
		}
	}
}

type tcpipForwardPayload struct {
	Addr string
	Port uint32
}

type tcpipForwardResponse struct {
	Port uint32
}

type forwardedTCPIPPayload struct {
	Addr       string
	Port       uint32
	OriginAddr string
	OriginPort uint32
}

func handleGlobalRequests(conn ssh.Conn, reqs <-chan *ssh.Request) {
	var listeners []net.Listener
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()

	for req := range reqs {
		switch req.Type {
		case "keepalive@openssh.org":
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
		case "tcpip-forward":
			listener, payload, err := startRemoteForwardListener(req.Payload)
			if err != nil {
				if req.WantReply {
					_ = req.Reply(false, nil)
				}
				continue
			}
			listeners = append(listeners, listener)
			if req.WantReply {
				_, portStr, splitErr := net.SplitHostPort(listener.Addr().String())
				if splitErr != nil {
					_ = req.Reply(false, nil)
					_ = listener.Close()
					continue
				}
				port, atoiErr := strconv.Atoi(portStr)
				if atoiErr != nil {
					_ = req.Reply(false, nil)
					_ = listener.Close()
					continue
				}
				var response []byte
				if payload.Port == 0 {
					response = ssh.Marshal(tcpipForwardResponse{Port: uint32(port)})
				}
				_ = req.Reply(true, response)
			}
			go acceptRemoteForward(listener, conn)
		default:
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	}
}

func startRemoteForwardListener(payloadBytes []byte) (net.Listener, tcpipForwardPayload, error) {
	var payload tcpipForwardPayload
	if err := ssh.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, payload, err
	}
	addr := payload.Addr
	if addr == "" {
		addr = "127.0.0.1"
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(addr, strconv.Itoa(int(payload.Port))))
	return listener, payload, err
}

func acceptRemoteForward(listener net.Listener, conn ssh.Conn) {
	for {
		remoteConn, err := listener.Accept()
		if err != nil {
			return
		}
		go openForwardedTCPIP(remoteConn, listener.Addr(), conn)
	}
}

func openForwardedTCPIP(remoteConn net.Conn, listenerAddr net.Addr, conn ssh.Conn) {
	destHost, destPort := splitTCPAddr(listenerAddr)
	originHost, originPort := splitTCPAddr(remoteConn.RemoteAddr())
	payload := forwardedTCPIPPayload{
		Addr:       destHost,
		Port:       uint32(destPort),
		OriginAddr: originHost,
		OriginPort: uint32(originPort),
	}
	channel, requests, err := conn.OpenChannel("forwarded-tcpip", ssh.Marshal(payload))
	if err != nil {
		_ = remoteConn.Close()
		return
	}
	go ssh.DiscardRequests(requests)
	proxyReadWriteCloser(channel, remoteConn)
}

func splitTCPAddr(addr net.Addr) (string, int) {
	if tcpAddr, ok := addr.(*net.TCPAddr); ok {
		return tcpAddr.IP.String(), tcpAddr.Port
	}
	host, portStr, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "127.0.0.1", 0
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return host, 0
	}
	return host, port
}

func (s *testSSHServer) handleSession(newChan ssh.NewChannel) {
	channel, requests, err := newChan.Accept()
	if err != nil {
		return
	}
	for req := range requests {
		switch req.Type {
		case "subsystem":
			var payload struct {
				Name string
			}
			if err := ssh.Unmarshal(req.Payload, &payload); err != nil || payload.Name != "sftp" {
				_ = req.Reply(false, nil)
				continue
			}
			_ = req.Reply(true, nil)
			server, err := pkgsftp.NewServer(channel, pkgsftp.WithServerWorkingDirectory(s.root))
			if err != nil {
				_ = channel.Close()
				return
			}
			_ = server.Serve()
			_ = channel.Close()
			return
		case "exec":
			var payload struct {
				Command string
			}
			if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
				_ = req.Reply(false, nil)
				continue
			}
			fields := strings.Fields(payload.Command)
			if len(fields) < 3 || fields[0] != "intun" || fields[1] != "relay" {
				_ = req.Reply(false, nil)
				continue
			}
			_ = req.Reply(true, nil)
			err := udprelay.RunCommand(context.Background(), fields[2:], channel, channel, channel.Stderr())
			status := uint32(0)
			if err != nil {
				status = 1
				_, _ = fmt.Fprintln(channel.Stderr(), err)
			}
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{Status: status}))
			_ = channel.Close()
			return
		default:
			_ = req.Reply(false, nil)
			continue
		}
	}
	_ = channel.Close()
}

type directTCPIPPayload struct {
	DestAddr   string
	DestPort   uint32
	OriginAddr string
	OriginPort uint32
}

func handleDirectTCPIP(newChan ssh.NewChannel) {
	var payload directTCPIPPayload
	if err := ssh.Unmarshal(newChan.ExtraData(), &payload); err != nil {
		_ = newChan.Reject(ssh.ConnectionFailed, "invalid direct-tcpip payload")
		return
	}
	target := net.JoinHostPort(payload.DestAddr, strconv.Itoa(int(payload.DestPort)))
	targetConn, err := net.Dial("tcp", target)
	if err != nil {
		_ = newChan.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	channel, requests, err := newChan.Accept()
	if err != nil {
		_ = targetConn.Close()
		return
	}
	go ssh.DiscardRequests(requests)
	proxyReadWriteCloser(channel, targetConn)
}

func proxyReadWriteCloser(left io.ReadWriteCloser, right io.ReadWriteCloser) {
	var once sync.Once
	closeBoth := func() {
		_ = left.Close()
		_ = right.Close()
	}
	go func() {
		_, _ = io.Copy(left, right)
		once.Do(closeBoth)
	}()
	_, _ = io.Copy(right, left)
	once.Do(closeBoth)
}
