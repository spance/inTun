package platform

import (
	"net"
	"sync/atomic"
)

type CountedConn struct {
	net.Conn
	upload   *atomic.Int64
	download *atomic.Int64
}

func NewCountedConn(conn net.Conn, upload, download *atomic.Int64) *CountedConn {
	return &CountedConn{
		Conn:     conn,
		upload:   upload,
		download: download,
	}
}

func (c *CountedConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.download.Add(int64(n))
	return n, err
}

func (c *CountedConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	c.upload.Add(int64(n))
	return n, err
}
