package platform

import (
	"fmt"
	"log/slog"
	"net"
)

func (e *SSHExecutor) startLocalForward(conn *SSHConnection, localPort, remotePort string) {
	listenAddr := tcpForwardAddr(localPort)
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		sshLog.Warn("local listen failed", "addr", listenAddr, slog.Any("error", err))
		conn.setError(fmt.Sprintf("LISTEN_FAILED: %v", err))
		if conn.client != nil {
			conn.client.Close()
		}
		return
	}
	conn.addForward(listener)
	sshLog.Info("local forward listening", "listen", listenAddr, "target", tcpForwardAddr(remotePort))

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

func (e *SSHExecutor) startRemoteForward(conn *SSHConnection, localAddr, remoteAddr string) {
	listenAddr := tcpForwardAddr(remoteAddr)
	listener, err := conn.client.Listen("tcp", listenAddr)
	if err != nil {
		sshLog.Warn("remote listen failed", "addr", listenAddr, slog.Any("error", err))
		conn.setError(fmt.Sprintf("REMOTE_LISTEN_FAILED: %v", err))
		if conn.client != nil {
			conn.client.Close()
		}
		return
	}
	conn.addForward(listener)
	sshLog.Info("remote forward listening", "listen", listenAddr, "target", tcpForwardAddr(localAddr))

	go func() {
		for {
			remoteConn, err := listener.Accept()
			if err != nil {
				return
			}
			go conn.handleRemoteForward(remoteConn, localAddr)
		}
	}()
}

func (e *SSHExecutor) startDynamicForward(conn *SSHConnection, localPort string) {
	listenAddr := tcpForwardAddr(localPort)
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		sshLog.Warn("dynamic listen failed", "addr", listenAddr, slog.Any("error", err))
		conn.setError(fmt.Sprintf("LISTEN_FAILED: %v", err))
		if conn.client != nil {
			conn.client.Close()
		}
		return
	}
	conn.addForward(listener)
	sshLog.Info("dynamic forward listening", "listen", listenAddr)

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

func (c *SSHConnection) handleLocalForward(localConn net.Conn, remotePort string) {
	defer localConn.Close()

	c.mu.RLock()
	client := c.client
	exited := c.exited
	c.mu.RUnlock()

	if exited || client == nil {
		return
	}

	remoteConn, err := client.Dial("tcp", tcpForwardAddr(remotePort))
	if err != nil {
		return
	}
	defer remoteConn.Close()

	countedRemote := NewCountedConn(remoteConn, &c.totalUpload, &c.totalDownload)
	proxyTCP(localConn, countedRemote)
}

func (c *SSHConnection) handleRemoteForward(remoteConn net.Conn, localAddr string) {
	defer remoteConn.Close()

	localConn, err := net.Dial("tcp", tcpForwardAddr(localAddr))
	if err != nil {
		return
	}
	defer localConn.Close()

	countedRemote := NewCountedConn(remoteConn, &c.totalUpload, &c.totalDownload)
	proxyTCP(localConn, countedRemote)
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

	socks5ErrReply := []byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0}

	if buf[0] != 0x05 || buf[1] != 0x01 {
		if buf[0] == 0x05 {
			localConn.Write(socks5ErrReply)
		}
		return
	}

	var target string
	switch buf[3] {
	case 0x01:
		if n < 10 {
			localConn.Write(socks5ErrReply)
			return
		}
		target = fmt.Sprintf("%d.%d.%d.%d:%d", buf[4], buf[5], buf[6], buf[7], int(buf[8])<<8|int(buf[9]))
	case 0x03:
		hostLen := int(buf[4])
		if n < 5+hostLen+2 {
			localConn.Write(socks5ErrReply)
			return
		}
		host := string(buf[5 : 5+hostLen])
		port := int(buf[5+hostLen])<<8 | int(buf[5+hostLen+1])
		target = fmt.Sprintf("%s:%d", host, port)
	default:
		localConn.Write(socks5ErrReply)
		return
	}

	c.mu.RLock()
	client := c.client
	exited := c.exited
	c.mu.RUnlock()

	if exited || client == nil {
		localConn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	remoteConn, err := client.Dial("tcp", target)
	if err != nil {
		localConn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remoteConn.Close()

	countedRemote := NewCountedConn(remoteConn, &c.totalUpload, &c.totalDownload)

	localConn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	proxyTCP(localConn, countedRemote)
}
