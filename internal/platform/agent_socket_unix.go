//go:build !windows

package platform

import (
	"net"
	"time"
)

func dialAgentSocket(path string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", path, timeout)
}
