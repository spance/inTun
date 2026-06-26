package sftp

import (
	"context"
	"sync"

	sftp "github.com/pkg/sftp"
)

type Client struct {
	client *sftp.Client
	mu     sync.Mutex
}

func NewClient(c *sftp.Client) *Client {
	return &Client{client: c}
}

func (s *Client) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
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
