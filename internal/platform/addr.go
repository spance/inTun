package platform

import (
	"net/netip"
	"strconv"
	"strings"
)

var loopbackAddr = netip.AddrFrom4([4]byte{127, 0, 0, 1})

func tcpForwardAddr(addr string) string {
	if strings.Contains(addr, ":") {
		return addr
	}
	port, err := strconv.ParseUint(addr, 10, 16)
	if err != nil {
		return "127.0.0.1:" + addr
	}
	return netip.AddrPortFrom(loopbackAddr, uint16(port)).String()
}
