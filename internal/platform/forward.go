package platform

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"
)

const forwardDialTimeout = 10 * time.Second

func (e *SSHExecutor) startLocalForward(conn *SSHConnection, localPort, remotePort string) error {
	listenAddr := tcpForwardAddr(localPort)
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		sshLog.Warn("local listen failed", "addr", listenAddr, slog.Any("error", err))
		return fmt.Errorf("LISTEN_FAILED: %w", err)
	}
	if !conn.addForward(listener) {
		return fmt.Errorf("SSH_CONNECTION_LOST: connection stopped before local forward started")
	}
	sshLog.Info("local forward listening", "listen", listenAddr, "target", tcpForwardAddr(remotePort))

	go func() {
		for {
			localConn, err := listener.Accept()
			if err != nil {
				conn.failForward(FailureSSHConnection, "local accept", err)
				return
			}
			go conn.handleLocalForward(localConn, remotePort)
		}
	}()
	return nil
}

func (e *SSHExecutor) startRemoteForward(conn *SSHConnection, localAddr, remoteAddr string) error {
	listenAddr := tcpForwardAddr(remoteAddr)
	listener, err := conn.client.Listen("tcp", listenAddr)
	if err != nil {
		sshLog.Warn("remote listen failed", "addr", listenAddr, slog.Any("error", err))
		return fmt.Errorf("REMOTE_LISTEN_FAILED: %w", err)
	}
	if !conn.addForward(listener) {
		return fmt.Errorf("SSH_CONNECTION_LOST: connection stopped before remote forward started")
	}
	sshLog.Info("remote forward listening", "listen", listenAddr, "target", tcpForwardAddr(localAddr))

	go func() {
		for {
			remoteConn, err := listener.Accept()
			if err != nil {
				conn.failForward(FailureRemoteListen, "remote accept", err)
				return
			}
			go conn.handleRemoteForward(remoteConn, localAddr)
		}
	}()
	return nil
}

func (e *SSHExecutor) startDynamicForward(conn *SSHConnection, localPort string) error {
	listenAddr := tcpForwardAddr(localPort)
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		sshLog.Warn("dynamic listen failed", "addr", listenAddr, slog.Any("error", err))
		return fmt.Errorf("LISTEN_FAILED: %w", err)
	}
	if !conn.addForward(listener) {
		return fmt.Errorf("SSH_CONNECTION_LOST: connection stopped before dynamic forward started")
	}
	sshLog.Info("dynamic forward listening", "listen", listenAddr)

	go func() {
		for {
			localConn, err := listener.Accept()
			if err != nil {
				conn.failForward(FailureSSHConnection, "dynamic accept", err)
				return
			}
			go conn.handleDynamicForward(localConn)
		}
	}()
	return nil
}

func (c *SSHConnection) handleLocalForward(localConn net.Conn, remotePort string) {
	defer localConn.Close()

	c.mu.RLock()
	client := c.client
	exited := c.exited
	c.mu.RUnlock()

	if exited || client == nil {
		return
	}

	dialCtx, cancel := context.WithTimeout(context.Background(), forwardDialTimeout)
	defer cancel()
	remoteConn, err := client.DialContext(dialCtx, "tcp", tcpForwardAddr(remotePort))
	if err != nil {
		sshLog.Debug("local forward target dial failed", "target", tcpForwardAddr(remotePort), slog.Any("error", err))
		return
	}
	defer remoteConn.Close()

	countedRemote := NewCountedConn(remoteConn, &c.totalUpload, &c.totalDownload)
	proxyTCP(localConn, countedRemote)
}

func (c *SSHConnection) handleRemoteForward(remoteConn net.Conn, localAddr string) {
	defer remoteConn.Close()

	dialer := net.Dialer{Timeout: forwardDialTimeout}
	localConn, err := dialer.Dial("tcp", tcpForwardAddr(localAddr))
	if err != nil {
		sshLog.Debug("remote forward target dial failed", "target", tcpForwardAddr(localAddr), slog.Any("error", err))
		return
	}
	defer localConn.Close()

	countedRemote := NewCountedConn(remoteConn, &c.totalUpload, &c.totalDownload)
	proxyTCP(localConn, countedRemote)
}

func (c *SSHConnection) handleDynamicForward(localConn net.Conn) {
	defer localConn.Close()

	target, err := negotiateSOCKS5(localConn)
	if err != nil {
		return
	}

	c.mu.RLock()
	client := c.client
	exited := c.exited
	c.mu.RUnlock()

	if exited || client == nil {
		_ = writeSOCKSReply(localConn, socksHostFail)
		return
	}

	dialCtx, cancel := context.WithTimeout(context.Background(), forwardDialTimeout)
	defer cancel()
	remoteConn, err := client.DialContext(dialCtx, "tcp", target)
	if err != nil {
		_ = writeSOCKSReply(localConn, socksNetworkFail)
		return
	}
	defer remoteConn.Close()

	countedRemote := NewCountedConn(remoteConn, &c.totalUpload, &c.totalDownload)

	if err := writeSOCKSReply(localConn, socksSuccess); err != nil {
		return
	}
	proxyTCP(localConn, countedRemote)
}
