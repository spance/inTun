package platform

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

type SSHConnection struct {
	client        *ssh.Client
	lastError     string
	exited        bool
	mu            sync.RWMutex
	stopOnce      sync.Once
	forwards      []io.Closer
	totalUpload   atomic.Int64
	totalDownload atomic.Int64
	knownHosts    *KnownHosts
	authCtx       *AuthContext
}

func (c *SSHConnection) Stop() error {
	var err error
	c.stopOnce.Do(func() {
		for _, f := range c.forwards {
			f.Close()
		}
		if c.client != nil {
			err = c.client.Close()
		}
		c.mu.Lock()
		c.exited = true
		c.mu.Unlock()
	})
	return err
}

func (c *SSHConnection) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return !c.exited
}

func (c *SSHConnection) Error() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastError
}

func (c *SSHConnection) WaitForReady(timeout time.Duration) bool {
	time.Sleep(timeout)
	return true
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

func (c *SSHConnection) setExited() {
	c.mu.Lock()
	c.exited = true
	c.mu.Unlock()
}

func (c *SSHConnection) setError(msg string) {
	c.mu.Lock()
	c.exited = true
	c.lastError = msg
	c.mu.Unlock()
}

type SSHExecutor struct{}

func newPlatformExecutor() Executor {
	return &SSHExecutor{}
}

func (e *SSHExecutor) Connect(authCtx *AuthContext, cfg *SSHConfig, tunnelType TunnelType, localPort, remotePort string) (Connection, error) {
	conn := &SSHConnection{
		authCtx: authCtx,
	}

	go e.connect(conn, cfg, tunnelType, localPort, remotePort)

	return conn, nil
}

func (e *SSHExecutor) connect(conn *SSHConnection, cfg *SSHConfig, tunnelType TunnelType, localPort, remotePort string) {
	if conn.authCtx != nil && conn.authCtx.Cancel != nil {
		select {
		case <-conn.authCtx.Cancel.Done():
			conn.setError("cancelled")
			return
		default:
		}
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

	addr := cfg.Host + ":" + cfg.Port
	client, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		conn.setError(fmt.Sprintf("SSH_CONNECTION_FAILED:%s: %v", cfg.Host, err))
		return
	}
	conn.client = client

	switch tunnelType {
	case Local:
		e.startLocalForward(conn, localPort, remotePort)
	case Remote:
		e.startRemoteForward(conn, localPort, remotePort)
	case Dynamic:
		e.startDynamicForward(conn, localPort)
	}

	go func() {
		client.Wait()
		conn.setExited()
	}()
}

func (e *SSHExecutor) getAuthMethods(identityFile string, authCtx *AuthContext, id int, user string, host string) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	var errs []string

	if identityFile != "" {
		path := expandPath(identityFile)
		key, err := e.loadPrivateKey(path)
		if err == nil {
			methods = append(methods, ssh.PublicKeys(key))
		} else {
			errs = append(errs, fmt.Sprintf("%s: %v", path, err))
		}
	}

	home, err := os.UserHomeDir()
	if err == nil {
		for _, keyFile := range []string{"id_rsa", "id_ed25519", "id_ecdsa", "id_dsa"} {
			path := filepath.Join(home, ".ssh", keyFile)
			key, err := e.loadPrivateKey(path)
			if err == nil {
				methods = append(methods, ssh.PublicKeys(key))
			} else {
				errs = append(errs, fmt.Sprintf("%s: %v", path, err))
			}
		}
	}

	if authCtx != nil && authCtx.RequestChan != nil {
		methods = append(methods, ssh.PasswordCallback(func() (string, error) {
			return e.promptPassword(authCtx, id, user, host)
		}))
		methods = append(methods, ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
			return e.handleKeyboardInteractive(authCtx, id, user, host, questions, echos)
		}))
	}

	if len(methods) == 0 {
		return nil, fmt.Errorf("no auth methods: %s", strings.Join(errs, "; "))
	}

	return methods, nil
}

func (e *SSHExecutor) promptPassword(authCtx *AuthContext, id int, user string, host string) (string, error) {
	timeout := authCtx.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	for retry := 0; retry < 3; retry++ {
		req := AuthRequest{
			ID:         id,
			Type:       AuthRequestPassword,
			Host:       user + "@" + host,
			RetryCount: retry,
			Response:   make(chan AuthResponse, 1),
		}

		select {
		case authCtx.RequestChan <- req:
		case <-authCtx.Cancel.Done():
			return "", errors.New("cancelled")
		case <-time.After(timeout):
			return "", errors.New("auth timeout")
		}

		select {
		case resp := <-req.Response:
			if !resp.Accept || resp.Password == "" {
				return "", errors.New("password cancelled")
			}
			return resp.Password, nil
		case <-authCtx.Cancel.Done():
			return "", errors.New("cancelled")
		case <-time.After(timeout):
			return "", errors.New("auth timeout")
		}
	}
	return "", errors.New("max password attempts")
}

func (e *SSHExecutor) handleKeyboardInteractive(authCtx *AuthContext, id int, user string, host string, questions []string, echos []bool) ([]string, error) {
	timeout := authCtx.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	answers := make([]string, len(questions))

	for retry := 0; retry < 3; retry++ {
		req := AuthRequest{
			ID:         id,
			Type:       AuthRequestPassword,
			Host:       user + "@" + host,
			RetryCount: retry,
			Response:   make(chan AuthResponse, 1),
		}

		select {
		case authCtx.RequestChan <- req:
		case <-authCtx.Cancel.Done():
			return nil, errors.New("cancelled")
		case <-time.After(timeout):
			return nil, errors.New("auth timeout")
		}

		select {
		case resp := <-req.Response:
			if !resp.Accept || resp.Password == "" {
				return nil, errors.New("password cancelled")
			}
			for i := range questions {
				answers[i] = resp.Password
			}
			return answers, nil
		case <-authCtx.Cancel.Done():
			return nil, errors.New("cancelled")
		case <-time.After(timeout):
			return nil, errors.New("auth timeout")
		}
	}
	return nil, errors.New("max password attempts")
}

func (e *SSHExecutor) startLocalForward(conn *SSHConnection, localPort, remotePort string) {
	listener, err := net.Listen("tcp", "127.0.0.1:"+localPort)
	if err != nil {
		conn.setError(fmt.Sprintf("LISTEN_FAILED: %v", err))
		if conn.client != nil {
			conn.client.Close()
		}
		return
	}
	conn.forwards = append(conn.forwards, listener)

	go func() {
		for {
			localConn, err := listener.Accept()
			if err != nil {
				return
			}
			go conn.handleLocalForward(localConn, remotePort)
		}
	}()
}

func (e *SSHExecutor) startRemoteForward(conn *SSHConnection, localPort, remotePort string) {
	listener, err := conn.client.Listen("tcp", "127.0.0.1:"+remotePort)
	if err != nil {
		conn.setError(fmt.Sprintf("REMOTE_LISTEN_FAILED: %v", err))
		if conn.client != nil {
			conn.client.Close()
		}
		return
	}
	conn.forwards = append(conn.forwards, listener)

	go func() {
		for {
			remoteConn, err := listener.Accept()
			if err != nil {
				return
			}
			go conn.handleRemoteForward(remoteConn, localPort)
		}
	}()
}

func (e *SSHExecutor) startDynamicForward(conn *SSHConnection, localPort string) {
	listener, err := net.Listen("tcp", "127.0.0.1:"+localPort)
	if err != nil {
		conn.setError(fmt.Sprintf("LISTEN_FAILED: %v", err))
		if conn.client != nil {
			conn.client.Close()
		}
		return
	}
	conn.forwards = append(conn.forwards, listener)

	go func() {
		for {
			localConn, err := listener.Accept()
			if err != nil {
				return
			}
			go conn.handleDynamicForward(localConn)
		}
	}()
}

func (e *SSHExecutor) loadPrivateKey(path string) (ssh.Signer, error) {
	keyBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	key, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, err
	}

	return key, nil
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

func (c *SSHConnection) handleLocalForward(localConn net.Conn, remotePort string) {
	defer localConn.Close()

	remoteConn, err := c.client.Dial("tcp", "127.0.0.1:"+remotePort)
	if err != nil {
		return
	}
	defer remoteConn.Close()

	countedRemote := NewCountedConn(remoteConn, &c.totalUpload, &c.totalDownload)

	go io.Copy(localConn, countedRemote)
	io.Copy(countedRemote, localConn)
}

func (c *SSHConnection) handleRemoteForward(remoteConn net.Conn, localPort string) {
	defer remoteConn.Close()

	localConn, err := net.Dial("tcp", "127.0.0.1:"+localPort)
	if err != nil {
		return
	}
	defer localConn.Close()

	countedRemote := NewCountedConn(remoteConn, &c.totalUpload, &c.totalDownload)

	go io.Copy(localConn, countedRemote)
	io.Copy(countedRemote, localConn)
}

func (c *SSHConnection) handleDynamicForward(localConn net.Conn) {
	defer localConn.Close()

	buf := make([]byte, 262)
	n, err := localConn.Read(buf)
	if err != nil || n < 3 {
		return
	}

	if buf[0] != 0x05 {
		return
	}

	localConn.Write([]byte{0x05, 0x00})

	buf = make([]byte, 262)
	n, err = localConn.Read(buf)
	if err != nil || n < 10 {
		return
	}

	if buf[0] != 0x05 || buf[1] != 0x01 {
		return
	}

	var target string
	switch buf[3] {
	case 0x01:
		if n < 10 {
			return
		}
		target = fmt.Sprintf("%d.%d.%d.%d:%d", buf[4], buf[5], buf[6], buf[7], int(buf[8])<<8|int(buf[9]))
	case 0x03:
		hostLen := int(buf[4])
		if n < 5+hostLen+2 {
			return
		}
		host := string(buf[5 : 5+hostLen])
		port := int(buf[5+hostLen])<<8 | int(buf[5+hostLen+1])
		target = fmt.Sprintf("%s:%d", host, port)
	default:
		return
	}

	remoteConn, err := c.client.Dial("tcp", target)
	if err != nil {
		localConn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remoteConn.Close()

	countedRemote := NewCountedConn(remoteConn, &c.totalUpload, &c.totalDownload)

	localConn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	go io.Copy(localConn, countedRemote)
	io.Copy(countedRemote, localConn)
}
