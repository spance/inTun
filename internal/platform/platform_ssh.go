package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

type SSHExecutor struct{}

func newPlatformExecutor() Executor {
	return &SSHExecutor{}
}

func (e *SSHExecutor) Connect(authCtx *AuthContext, cfg *SSHConfig, spec ForwardSpec) (Connection, error) {
	baseCtx := context.Background()
	authCopy := AuthContext{}
	if authCtx != nil {
		authCopy = *authCtx
		if authCtx.Cancel != nil {
			baseCtx = authCtx.Cancel
		}
	}
	runCtx, cancel := context.WithCancel(baseCtx)
	authCopy.Cancel = runCtx
	conn := &SSHConnection{
		authCtx: &authCopy,
		cancel:  cancel,
	}

	go e.connect(conn, cfg, spec)

	return conn, nil
}

func (e *SSHExecutor) connect(conn *SSHConnection, cfg *SSHConfig, spec ForwardSpec) {
	if conn.authCtx != nil && conn.authCtx.Cancel != nil {
		select {
		case <-conn.authCtx.Cancel.Done():
			conn.setError("cancelled")
			return
		default:
		}
	}
	if err := spec.Validate(); err != nil {
		conn.setError("INVALID_FORWARD: " + err.Error())
		return
	}

	if cfg == nil {
		conn.setError("SSH_CONNECTION_FAILED: no SSH configuration provided")
		return
	}
	if cfg.Host == "" {
		conn.setError("SSH_CONNECTION_FAILED: no host specified")
		return
	}

	knownHosts, err := NewKnownHosts()
	if err != nil {
		conn.setError(fmt.Sprintf("KNOWN_HOSTS_ERROR: %v", err))
		return
	}
	conn.knownHosts = knownHosts

	var (
		upstream *ssh.Client
		jumps    []*ssh.Client
	)
	for index := range cfg.ProxyJumps {
		jumpCfg := cfg.ProxyJumps[index]
		jump, dialErr := e.dialSSHClient(conn.authCtx.Cancel, conn.authCtx, &jumpCfg, knownHosts, upstream)
		if dialErr != nil {
			closeSSHClients(jumps)
			conn.setError(fmt.Sprintf("SSH_CONNECTION_FAILED: ProxyJump %s: %v", jumpCfg.Host, dialErr))
			return
		}
		jumps = append(jumps, jump)
		upstream = jump
	}

	client, err := e.dialSSHClient(conn.authCtx.Cancel, conn.authCtx, cfg, knownHosts, upstream)
	if err != nil {
		closeSSHClients(jumps)
		conn.setError(fmt.Sprintf("SSH_CONNECTION_FAILED:%s: %v", cfg.Host, err))
		return
	}
	conn.mu.Lock()
	if conn.exited {
		conn.mu.Unlock()
		_ = client.Close()
		closeSSHClients(jumps)
		return
	}
	conn.client = client
	conn.keepaliveStop = make(chan struct{})
	for _, jump := range jumps {
		conn.forwards = append(conn.forwards, jump)
	}
	conn.mu.Unlock()

	go conn.sendKeepalive()

	if err := e.startForward(conn, spec); err != nil {
		conn.failConnection(err.Error())
		return
	}
	conn.setReady()

	go func() {
		err := client.Wait()
		if err != nil {
			sshLog.Warn("connection closed",
				"user", cfg.User,
				"host", cfg.Host,
				"port", cfg.Port,
				slog.Any("error", err),
			)
			conn.setConnectionLost("SSH_CONNECTION_LOST: " + err.Error())
			return
		}
		conn.setConnectionLost("SSH_CONNECTION_LOST: connection closed")
	}()
}

func (e *SSHExecutor) dialSSHClient(ctx context.Context, authCtx *AuthContext, cfg *SSHConfig, knownHosts *KnownHosts, upstream *ssh.Client) (*ssh.Client, error) {
	if cfg == nil || cfg.Host == "" {
		return nil, errors.New("no host specified")
	}
	port := cfg.Port
	if port == "" {
		port = "22"
	}
	authMethods, authClosers, err := e.getAuthMethods(cfg, authCtx, authCtx.TunnelID)
	defer closeAll(authClosers)
	if err != nil {
		return nil, fmt.Errorf("authentication methods: %w", err)
	}
	if len(authMethods) == 0 {
		return nil, errors.New("authentication methods: none available")
	}

	sshConfig := &ssh.ClientConfig{
		User: cfg.User,
		Auth: authMethods,
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return knownHosts.VerifyHostKey(authCtx, authCtx.TunnelID, cfg.Host, port, key)
		},
		Timeout: 10 * time.Second,
	}
	addr := net.JoinHostPort(cfg.Host, port)
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var netConn net.Conn
	if upstream == nil {
		dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
		netConn, err = dialer.DialContext(dialCtx, "tcp", addr)
	} else {
		netConn, err = upstream.DialContext(dialCtx, "tcp", addr)
	}
	if err != nil {
		return nil, err
	}

	handshakeDone := make(chan struct{})
	go func() {
		select {
		case <-dialCtx.Done():
			_ = netConn.Close()
		case <-handshakeDone:
		}
	}()
	sshConn, chans, reqs, err := ssh.NewClientConn(netConn, addr, sshConfig)
	close(handshakeDone)
	if err != nil {
		_ = netConn.Close()
		return nil, err
	}
	return ssh.NewClient(sshConn, chans, reqs), nil
}

func closeSSHClients(clients []*ssh.Client) {
	for index := len(clients) - 1; index >= 0; index-- {
		_ = clients[index].Close()
	}
}

func closeAll(closers []io.Closer) {
	for _, closer := range closers {
		if closer != nil {
			_ = closer.Close()
		}
	}
}
