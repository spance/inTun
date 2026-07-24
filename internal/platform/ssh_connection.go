package platform

import (
	"context"
	"fmt"
	"io"
	"log/slog"
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
	cancel        context.CancelFunc
	keepaliveStop chan struct{}
	keepaliveMu   sync.Mutex
}

var sshLog = logging.Default().With("component", "ssh")

const sshKeepaliveTimeout = 5 * time.Second

func (c *SSHConnection) Stop() error {
	var err error
	c.stopOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
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
	latency, err := c.sendKeepaliveRequest()
	if err != nil {
		c.setConnectionLost("SSH_KEEPALIVE_FAILED: " + err.Error())
		return 0
	}
	return latency
}

func (c *SSHConnection) sendKeepalive() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.keepaliveStop:
			return
		case <-ticker.C:
			_, err := c.sendKeepaliveRequest()
			if err != nil {
				if c.setConnectionLost("SSH_KEEPALIVE_FAILED: " + err.Error()) {
					sshLog.Warn("keepalive failed", slog.Any("error", err))
				}
				return
			}
		}
	}
}

type globalRequester interface {
	SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error)
}

func (c *SSHConnection) sendKeepaliveRequest() (time.Duration, error) {
	c.keepaliveMu.Lock()
	defer c.keepaliveMu.Unlock()

	c.mu.RLock()
	if c.exited || c.client == nil {
		c.mu.RUnlock()
		return 0, fmt.Errorf("connection not available")
	}
	client := c.client
	c.mu.RUnlock()
	return sendGlobalRequestWithTimeout(client, client.Close, sshKeepaliveTimeout)
}

func sendGlobalRequestWithTimeout(requester globalRequester, closeRequest func() error, timeout time.Duration) (time.Duration, error) {
	if requester == nil {
		return 0, fmt.Errorf("requester is not available")
	}
	if timeout <= 0 {
		timeout = sshKeepaliveTimeout
	}
	start := time.Now()
	result := make(chan error, 1)
	go func() {
		_, _, err := requester.SendRequest("keepalive@openssh.org", true, nil)
		result <- err
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-result:
		if err != nil {
			return 0, err
		}
		return time.Since(start), nil
	case <-timer.C:
		if closeRequest != nil {
			_ = closeRequest()
		}
		return 0, fmt.Errorf("request timed out after %s", timeout)
	}
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

func (c *SSHConnection) failForward(code FailureCode, op string, err error) {
	if err == nil {
		return
	}
	c.mu.RLock()
	exited := c.exited
	c.mu.RUnlock()
	if exited {
		return
	}
	c.failConnection(NewFailure(code, op, err).Error())
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
