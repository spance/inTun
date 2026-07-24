package platform

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

var loopbackAddr = netip.AddrFrom4([4]byte{127, 0, 0, 1})

type ForwardAddressOptions struct {
	AllowHost     bool
	AllowZeroPort bool
}

func ParseForwardAddress(value string, options ForwardAddressOptions) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("address is required")
	}
	if !strings.Contains(value, ":") {
		port, err := parsePort(value, options.AllowZeroPort)
		if err != nil {
			return "", err
		}
		return netip.AddrPortFrom(loopbackAddr, port).String(), nil
	}
	if !options.AllowHost {
		return "", fmt.Errorf("only a port number is allowed")
	}
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return "", fmt.Errorf("invalid host:port %q: %w", value, err)
	}
	if host == "" {
		return "", fmt.Errorf("host is required")
	}
	port, err := parsePort(portText, options.AllowZeroPort)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}

func tcpForwardAddr(addr string) string {
	normalized, err := ParseForwardAddress(addr, ForwardAddressOptions{
		AllowHost:     true,
		AllowZeroPort: true,
	})
	if err != nil {
		if !strings.Contains(addr, ":") {
			return net.JoinHostPort(loopbackAddr.String(), addr)
		}
		return addr
	}
	return normalized
}

func parsePort(value string, allowZero bool) (uint16, error) {
	port, err := strconv.ParseUint(value, 10, 16)
	if err != nil || (!allowZero && port == 0) {
		return 0, fmt.Errorf("invalid port %q", value)
	}
	return uint16(port), nil
}
