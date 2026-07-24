//go:build windows

package platform

import (
	"context"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

func dialAgentSocket(path string, timeout time.Duration) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return winio.DialPipeContext(ctx, path)
}
