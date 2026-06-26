package sftp

import (
	"context"
	"io"
	"os"
	"sync"

	sftp "github.com/pkg/sftp"
)

type remoteFileSystem interface {
	io.Closer
	Rename(oldPath, newPath string) error
	Open(path string) (io.ReadCloser, error)
	Create(path string) (io.WriteCloser, error)
	MkdirAll(path string) error
	Lstat(path string) (os.FileInfo, error)
	ReadDirContext(ctx context.Context, path string) ([]os.FileInfo, error)
}

type sftpRemoteFS struct {
	client *sftp.Client
}

func (fs sftpRemoteFS) Close() error {
	return fs.client.Close()
}

func (fs sftpRemoteFS) Rename(oldPath, newPath string) error {
	return fs.client.Rename(oldPath, newPath)
}

func (fs sftpRemoteFS) Open(path string) (io.ReadCloser, error) {
	return fs.client.Open(path)
}

func (fs sftpRemoteFS) Create(path string) (io.WriteCloser, error) {
	return fs.client.Create(path)
}

func (fs sftpRemoteFS) MkdirAll(path string) error {
	return fs.client.MkdirAll(path)
}

func (fs sftpRemoteFS) Lstat(path string) (os.FileInfo, error) {
	return fs.client.Lstat(path)
}

func (fs sftpRemoteFS) ReadDirContext(ctx context.Context, path string) ([]os.FileInfo, error) {
	return fs.client.ReadDirContext(ctx, path)
}

type Client struct {
	client remoteFileSystem
	mu     sync.Mutex
}

func NewClient(c *sftp.Client) *Client {
	if c == nil {
		return &Client{}
	}
	return &Client{client: sftpRemoteFS{client: c}}
}

func newClientWithFS(fs remoteFileSystem) *Client {
	return &Client{client: fs}
}

func (s *Client) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return nil
	}
	return s.client.Close()
}

func (s *Client) Rename(ctx context.Context, oldPath, newPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := checkCanceled(ctx); err != nil {
		return err
	}
	return s.client.Rename(oldPath, newPath)
}
