package platform

import (
	"io"
	"net"

	"golang.org/x/sync/errgroup"
)

func proxyTCP(left, right net.Conn) {
	var group errgroup.Group
	group.Go(func() error {
		_, err := io.Copy(left, right)
		closeWrite(left)
		closeRead(right)
		return err
	})
	group.Go(func() error {
		_, err := io.Copy(right, left)
		closeWrite(right)
		closeRead(left)
		return err
	})
	_ = group.Wait()
	_ = left.Close()
	_ = right.Close()
}

func closeWrite(conn net.Conn) {
	if halfCloser, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = halfCloser.CloseWrite()
	}
}

func closeRead(conn net.Conn) {
	if halfCloser, ok := conn.(interface{ CloseRead() error }); ok {
		_ = halfCloser.CloseRead()
	}
}
