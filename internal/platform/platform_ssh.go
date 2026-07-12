package platform

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	sftp "github.com/pkg/sftp"
	"github.com/spance/intun/internal/logging"
	"golang.org/x/crypto/ssh"
)

type SSHConnection struct {
	client        *ssh.Client
	lastError     string
	exited        bool
	ready         bool
	mu            sync.RWMutex
	stopOnce      sync.Once
	forwards      []io.Closer
	totalUpload   atomic.Int64
	totalDownload atomic.Int64
	knownHosts    *KnownHosts
	authCtx       *AuthContext
	keepaliveStop chan struct{}
}

func (c *SSHConnection) Stop() error {
	var err error
	c.stopOnce.Do(func() {
		c.mu.Lock()
		keepaliveStop := c.keepaliveStop
		forwards := append([]io.Closer(nil), c.forwards...)
		client := c.client
		c.exited = true
		c.ready = false
		c.mu.Unlock()

		if keepaliveStop != nil {
			close(keepaliveStop)
		}
		for _, f := range forwards {
			_ = f.Close()
		}
		if client != nil {
			err = client.Close()
		}
	})
	return err
}

func (c *SSHConnection) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ready && !c.exited
}

func (c *SSHConnection) Error() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastError
}

func (c *SSHConnection) GetStats() (int64, int64) {
	return c.totalUpload.Load(), c.totalDownload.Load()
}

func (c *SSHConnection) Ping() time.Duration {
	c.mu.RLock()
	if c.exited || c.client == nil {
		c.mu.RUnlock()
		return 0
	}
	client := c.client
	c.mu.RUnlock()

	start := time.Now()
	_, _, err := client.SendRequest("keepalive@openssh.org", true, nil)
	if err != nil {
		return 0
	}
	return time.Since(start)
}

func (c *SSHConnection) sendKeepalive() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.keepaliveStop:
			return
		case <-ticker.C:
			c.mu.RLock()
			if c.exited || c.client == nil {
				c.mu.RUnlock()
				return
			}
			client := c.client
			c.mu.RUnlock()

			_, _, err := client.SendRequest("keepalive@openssh.org", true, nil)
			if err != nil {
				sshLog.Warn("keepalive failed", slog.Any("error", err))
				c.failConnection("SSH_KEEPALIVE_FAILED: " + err.Error())
				return
			}
		}
	}
}

func (c *SSHConnection) setExited() {
	c.mu.Lock()
	c.exited = true
	c.mu.Unlock()
}

func (c *SSHConnection) setReady() {
	c.mu.Lock()
	if !c.exited && c.lastError == "" {
		c.ready = true
	}
	c.mu.Unlock()
}

func (c *SSHConnection) setError(msg string) {
	c.mu.Lock()
	c.exited = true
	c.ready = false
	c.lastError = msg
	c.mu.Unlock()
}

func (c *SSHConnection) setConnectionLost(msg string) bool {
	c.mu.Lock()
	if c.exited || c.lastError != "" {
		c.mu.Unlock()
		return false
	}
	c.exited = true
	c.ready = false
	c.lastError = msg
	c.mu.Unlock()
	c.closeForwardResources()
	return true
}

func (c *SSHConnection) failConnection(msg string) {
	c.setError(msg)
	c.closeForwardResources()
}

func (c *SSHConnection) closeForwardResources() {
	c.mu.RLock()
	forwards := append([]io.Closer(nil), c.forwards...)
	client := c.client
	c.mu.RUnlock()
	for _, forward := range forwards {
		_ = forward.Close()
	}
	if client != nil {
		_ = client.Close()
	}
}

func (c *SSHConnection) addForward(f io.Closer) bool {
	c.mu.Lock()
	if c.exited {
		c.mu.Unlock()
		_ = f.Close()
		return false
	}
	c.forwards = append(c.forwards, f)
	c.mu.Unlock()
	return true
}

func (c *SSHConnection) NewSFTPClient() (interface{}, error) {
	c.mu.RLock()
	client := c.client
	exited := c.exited
	c.mu.RUnlock()
	if exited || client == nil {
		return nil, fmt.Errorf("SSH_CONNECTION_LOST: connection not available")
	}
	return sftp.NewClient(client)
}

type SSHExecutor struct{}

var sshLog = logging.Default().With("component", "ssh")

func newPlatformExecutor() Executor {
	return &SSHExecutor{}
}

func (e *SSHExecutor) Connect(authCtx *AuthContext, cfg *SSHConfig, spec ForwardSpec) (Connection, error) {
	conn := &SSHConnection{
		authCtx: authCtx,
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

	originalHost := cfg.Host
	originalPort := cfg.Port
	if originalPort == "" {
		originalPort = "22"
	}

	port := cfg.Port
	if port == "" {
		port = "22"
	}

	sshConfig := &ssh.ClientConfig{
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return knownHosts.VerifyHostKey(conn.authCtx, 0, originalHost, originalPort, key)
		},
		Timeout: 10 * time.Second,
	}

	if cfg.User != "" {
		sshConfig.User = cfg.User
	}

	authMethods, authErr := e.getAuthMethods(cfg.IdentityFile, conn.authCtx, 0, cfg.User, cfg.Host)
	if len(authMethods) == 0 {
		conn.setError(fmt.Sprintf("SSH_AUTH_FAILED: %v", authErr))
		return
	}
	sshConfig.Auth = authMethods

	addr := cfg.Host + ":" + port
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	netConn, err := dialer.Dial("tcp", addr)
	if err != nil {
		conn.setError(fmt.Sprintf("SSH_CONNECTION_FAILED:%s: %v", cfg.Host, err))
		return
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(netConn, addr, sshConfig)
	if err != nil {
		netConn.Close()
		conn.setError(fmt.Sprintf("SSH_CONNECTION_FAILED:%s: %v", cfg.Host, err))
		return
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	conn.mu.Lock()
	if conn.exited {
		conn.mu.Unlock()
		_ = client.Close()
		return
	}
	conn.client = client
	conn.keepaliveStop = make(chan struct{})
	conn.mu.Unlock()

	go conn.sendKeepalive()

	if err := e.startForward(conn, spec); err != nil {
		conn.setError(err.Error())
		_ = client.Close()
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
		}
		conn.setExited()
	}()
}
