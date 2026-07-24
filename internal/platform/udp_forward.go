package platform

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/spance/intun/internal/logging"
	"github.com/spance/intun/internal/udprelay"
	"golang.org/x/crypto/ssh"
)

const udpRelayReadyTimeout = 10 * time.Second

type udpForwarder struct{}

func (udpForwarder) Start(conn *SSHConnection, spec ForwardSpec) error {
	conn.mu.RLock()
	client := conn.client
	exited := conn.exited
	conn.mu.RUnlock()
	if exited || client == nil {
		return fmt.Errorf("UDP_RELAY_FAILED: SSH connection not available")
	}

	switch spec.Type {
	case Local:
		return startLocalUDPForward(conn, client, spec)
	case Remote:
		return startRemoteUDPForward(conn, client, spec)
	default:
		return fmt.Errorf("UNSUPPORTED_FORWARD: %s over UDP", spec.Type)
	}
}

func startLocalUDPForward(conn *SSHConnection, client *ssh.Client, spec ForwardSpec) error {
	listenAddr, err := net.ResolveUDPAddr("udp", udpForwardAddr(spec.LocalAddr))
	if err != nil {
		return fmt.Errorf("UDP_LISTEN_FAILED: resolve %s: %w", spec.LocalAddr, err)
	}
	listener, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		return fmt.Errorf("UDP_LISTEN_FAILED: %w", err)
	}

	remote, err := startRemoteUDPRelay(client, "target", spec.RemoteAddr)
	if err != nil {
		_ = listener.Close()
		return err
	}
	transport := newCountingFrameTransport(remote.transport, &conn.totalUpload, &conn.totalDownload)
	peer := udprelay.NewPeerRelay(listener, transport, udprelay.RelayOptions{}, localPeerHooks())
	runtime := newUDPForwardRuntime(remote.session, listener, "", peer.Serve, func(err error) {
		conn.failConnection("UDP_RELAY_FAILED: " + err.Error())
	})
	if !conn.addForward(runtime) {
		return fmt.Errorf("SSH_CONNECTION_LOST: connection stopped before UDP forward started")
	}
	sshLog.Info("local UDP forward listening", "listen", listener.LocalAddr().String(), "target", udpForwardAddr(spec.RemoteAddr))
	runtime.start()
	return nil
}

func startRemoteUDPForward(conn *SSHConnection, client *ssh.Client, spec ForwardSpec) error {
	localTarget := udpForwardAddr(spec.LocalAddr)
	if _, err := net.ResolveUDPAddr("udp", localTarget); err != nil {
		return fmt.Errorf("UDP_TARGET_FAILED: resolve %s: %w", spec.LocalAddr, err)
	}
	remote, err := startRemoteUDPRelay(client, "listen", spec.RemoteAddr)
	if err != nil {
		return err
	}
	transport := newCountingFrameTransport(remote.transport, &conn.totalUpload, &conn.totalDownload)
	logLimiter := logging.NewRateLimiter(5 * time.Second)
	serveTarget := func(ctx context.Context) error {
		return udprelay.ServeTarget(ctx, transport, localTarget, udprelay.RelayOptions{}, udprelay.TargetHooks{
			OnAssociationError: func(id uint32, err error) {
				if allowed, suppressed := logLimiter.Allow("target-association"); allowed {
					sshLog.Warn("UDP target association failed", "association_id", id, "error", err, "suppressed", suppressed)
				}
			},
		})
	}
	boundAddr := strings.TrimSpace(string(remote.ready.Payload))
	if boundAddr == "" {
		boundAddr = udpForwardAddr(spec.RemoteAddr)
	}
	runtime := newUDPForwardRuntime(remote.session, nil, boundAddr, serveTarget, func(err error) {
		conn.failConnection("UDP_RELAY_FAILED: " + err.Error())
	})
	if !conn.addForward(runtime) {
		return fmt.Errorf("SSH_CONNECTION_LOST: connection stopped before remote UDP forward started")
	}
	sshLog.Info("remote UDP forward listening", "listen", boundAddr, "target", localTarget)
	runtime.start()
	return nil
}

func localPeerHooks() udprelay.PeerHooks {
	logLimiter := logging.NewRateLimiter(5 * time.Second)
	return udprelay.PeerHooks{
		OnDrop: func(peer *net.UDPAddr, err error) {
			if allowed, suppressed := logLimiter.Allow("peer-drop"); allowed {
				sshLog.Warn("dropping UDP datagram", "peer", peer.String(), "error", err, "suppressed", suppressed)
			}
		},
		OnAssociationError: func(id uint32, message string) {
			if allowed, suppressed := logLimiter.Allow("peer-association"); allowed {
				sshLog.Warn("UDP association failed", "association_id", id, "error", strings.TrimSpace(message), "suppressed", suppressed)
			}
		},
	}
}

type remoteUDPRelay struct {
	session   *ssh.Session
	transport *udprelay.StreamTransport
	ready     udprelay.Frame
}

func startRemoteUDPRelay(client *ssh.Client, mode, address string) (*remoteUDPRelay, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("UDP_RELAY_FAILED: create remote session: %w", err)
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("UDP_RELAY_FAILED: open relay input: %w", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("UDP_RELAY_FAILED: open relay output: %w", err)
	}
	stderr := newBoundedBuffer(4096)
	session.Stderr = stderr
	addressToken := base64.RawURLEncoding.EncodeToString([]byte(udpForwardAddr(address)))
	command := fmt.Sprintf("intun relay udp %s --address-token %s", mode, addressToken)
	if err := session.Start(command); err != nil {
		_ = session.Close()
		return nil, fmt.Errorf("UDP_RELAY_FAILED: start remote relay: %w", err)
	}

	transport := udprelay.NewStreamTransport(stdout, stdin, session)
	ready, err := readRelayReady(transport, session)
	if err != nil {
		_ = transport.Close()
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return nil, fmt.Errorf("UDP_RELAY_FAILED: %w: %s", err, detail)
		}
		return nil, fmt.Errorf("UDP_RELAY_FAILED: %w", err)
	}
	if ready.Type == udprelay.FrameError {
		_ = transport.Close()
		return nil, fmt.Errorf("UDP_RELAY_FAILED: %s", strings.TrimSpace(string(ready.Payload)))
	}
	if ready.Type != udprelay.FrameReady {
		_ = transport.Close()
		return nil, fmt.Errorf("UDP_RELAY_FAILED: unexpected startup frame %d", ready.Type)
	}
	return &remoteUDPRelay{session: session, transport: transport, ready: ready}, nil
}

func udpForwardAddr(addr string) string {
	return tcpForwardAddr(addr)
}

type relayReadyResult struct {
	frame udprelay.Frame
	err   error
}

func readRelayReady(transport udprelay.FrameTransport, session *ssh.Session) (udprelay.Frame, error) {
	result := make(chan relayReadyResult, 1)
	go func() {
		frame, err := transport.ReadFrame()
		result <- relayReadyResult{frame: frame, err: err}
	}()
	timer := time.NewTimer(udpRelayReadyTimeout)
	defer timer.Stop()
	select {
	case ready := <-result:
		if ready.err != nil {
			return udprelay.Frame{}, fmt.Errorf("remote relay did not become ready: %w", ready.err)
		}
		return ready.frame, nil
	case <-timer.C:
		_ = session.Close()
		return udprelay.Frame{}, fmt.Errorf("remote relay readiness timed out")
	}
}
