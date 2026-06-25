package platform

import (
	"io"
	"net"
	"sync"

	"golang.org/x/sync/errgroup"
)

func proxyTCP(left, right net.Conn) {
	var once sync.Once
	closeBoth := func() {
		_ = left.Close()
		_ = right.Close()
	}

	var group errgroup.Group
	group.Go(func() error {
		_, err := io.Copy(left, right)
		once.Do(closeBoth)
		return err
	})
	group.Go(func() error {
		_, err := io.Copy(right, left)
		once.Do(closeBoth)
		return err
	})
	_ = group.Wait()
}
