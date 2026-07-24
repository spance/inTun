package platform

import (
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

const (
	socksVersion       = 0x05
	socksNoAuth        = 0x00
	socksNoAcceptable  = 0xff
	socksConnect       = 0x01
	socksIPv4          = 0x01
	socksDomain        = 0x03
	socksIPv6          = 0x04
	socksSuccess       = 0x00
	socksGeneralFail   = 0x01
	socksNetworkFail   = 0x03
	socksHostFail      = 0x04
	socksCommandFail   = 0x07
	socksAddressFail   = 0x08
	socksHandshakeTime = 15 * time.Second
)

var errSOCKSNoAuth = errors.New("SOCKS5 client does not offer no-authentication")

func negotiateSOCKS5(conn net.Conn) (string, error) {
	if err := conn.SetDeadline(time.Now().Add(socksHandshakeTime)); err != nil {
		return "", err
	}
	defer conn.SetDeadline(time.Time{})

	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return "", err
	}
	if header[0] != socksVersion || header[1] == 0 {
		return "", fmt.Errorf("invalid SOCKS5 greeting")
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return "", err
	}
	if !containsByte(methods, socksNoAuth) {
		_ = writeSOCKSMethod(conn, socksNoAcceptable)
		return "", errSOCKSNoAuth
	}
	if err := writeSOCKSMethod(conn, socksNoAuth); err != nil {
		return "", err
	}

	request := make([]byte, 4)
	if _, err := io.ReadFull(conn, request); err != nil {
		return "", err
	}
	if request[0] != socksVersion || request[2] != 0 {
		_ = writeSOCKSReply(conn, socksGeneralFail)
		return "", fmt.Errorf("invalid SOCKS5 request")
	}
	if request[1] != socksConnect {
		_ = writeSOCKSReply(conn, socksCommandFail)
		return "", fmt.Errorf("unsupported SOCKS5 command %d", request[1])
	}

	host, err := readSOCKSHost(conn, request[3])
	if err != nil {
		_ = writeSOCKSReply(conn, socksAddressFail)
		return "", err
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBytes); err != nil {
		return "", err
	}
	port := int(portBytes[0])<<8 | int(portBytes[1])
	if port == 0 {
		_ = writeSOCKSReply(conn, socksAddressFail)
		return "", fmt.Errorf("invalid SOCKS5 port")
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", port)), nil
}

func readSOCKSHost(conn net.Conn, addressType byte) (string, error) {
	switch addressType {
	case socksIPv4:
		raw := make([]byte, net.IPv4len)
		if _, err := io.ReadFull(conn, raw); err != nil {
			return "", err
		}
		return net.IP(raw).String(), nil
	case socksIPv6:
		raw := make([]byte, net.IPv6len)
		if _, err := io.ReadFull(conn, raw); err != nil {
			return "", err
		}
		return net.IP(raw).String(), nil
	case socksDomain:
		length := []byte{0}
		if _, err := io.ReadFull(conn, length); err != nil {
			return "", err
		}
		if length[0] == 0 {
			return "", fmt.Errorf("empty SOCKS5 domain")
		}
		raw := make([]byte, int(length[0]))
		if _, err := io.ReadFull(conn, raw); err != nil {
			return "", err
		}
		return string(raw), nil
	default:
		return "", fmt.Errorf("unsupported SOCKS5 address type %d", addressType)
	}
}

func writeSOCKSMethod(conn net.Conn, method byte) error {
	_, err := conn.Write([]byte{socksVersion, method})
	return err
}

func writeSOCKSReply(conn net.Conn, code byte) error {
	_, err := conn.Write([]byte{socksVersion, code, 0x00, socksIPv4, 0, 0, 0, 0, 0, 0})
	return err
}

func containsByte(values []byte, target byte) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
