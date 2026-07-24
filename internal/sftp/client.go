package sftp

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	sftp "github.com/pkg/sftp"
)

type remoteFileSystem interface {
	io.Closer
	Rename(oldPath, newPath string) error
	Open(path string) (io.ReadCloser, error)
	Create(path string) (io.WriteCloser, error)
	Remove(path string) error
	MkdirAll(path string) error
	Lstat(path string) (os.FileInfo, error)
	ReadDirContext(ctx context.Context, path string) ([]os.FileInfo, error)
	PosixRename(oldPath, newPath string) error
	HasExtension(name string) (string, bool)
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

func (fs sftpRemoteFS) Remove(path string) error {
	return fs.client.Remove(path)
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

func (fs sftpRemoteFS) PosixRename(oldPath, newPath string) error {
	return fs.client.PosixRename(oldPath, newPath)
}

func (fs sftpRemoteFS) HasExtension(name string) (string, bool) {
	return fs.client.HasExtension(name)
}

type Client struct {
	client    remoteFileSystem
	initOnce  sync.Once
	op        chan struct{}
	stateMu   sync.RWMutex
	closed    bool
	closeOnce sync.Once
	closeDone chan struct{}
	closeErr  error
}

func NewClient(c *sftp.Client) *Client {
	if c == nil {
		return newClientWithFS(nil)
	}
	return newClientWithFS(sftpRemoteFS{client: c})
}

func newClientWithFS(fs remoteFileSystem) *Client {
	client := &Client{client: fs}
	client.initialize()
	return client
}

func (s *Client) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.CloseContext(ctx)
}

func (s *Client) CloseContext(ctx context.Context) error {
	s.initialize()
	s.stateMu.Lock()
	s.closed = true
	client := s.client
	s.stateMu.Unlock()

	s.closeOnce.Do(func() {
		go func() {
			if client != nil {
				s.closeErr = client.Close()
			}
			close(s.closeDone)
		}()
	})
	select {
	case <-s.closeDone:
		return s.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Client) Rename(ctx context.Context, oldPath, newPath string) error {
	_, done, err := s.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer done()
	exists, err := remotePathExists(s.client, newPath)
	if err != nil {
		return err
	}
	if exists {
		return ErrOverwriteConfirmationRequired
	}
	return s.client.Rename(oldPath, newPath)
}

func (s *Client) initialize() {
	s.initOnce.Do(func() {
		s.op = make(chan struct{}, 1)
		s.closeDone = make(chan struct{})
	})
}

func (s *Client) beginOperation(ctx context.Context) (remoteFileSystem, func(), error) {
	s.initialize()
	if err := checkCanceled(ctx); err != nil {
		return nil, nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case s.op <- struct{}{}:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	s.stateMu.RLock()
	client := s.client
	closed := s.closed
	s.stateMu.RUnlock()
	if closed || client == nil {
		<-s.op
		return nil, nil, errors.New("SFTP client is closed")
	}
	return client, func() { <-s.op }, nil
}
