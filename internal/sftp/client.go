package sftp

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

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

const maxDirEntries = 1000

func (s *Client) ReadRemoteDir(ctx context.Context, path string) ([]FileEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := checkCanceled(ctx); err != nil {
		return nil, err
	}

	infos, err := s.client.ReadDirContext(ctx, path)
	if err != nil {
		return nil, err
	}

	if len(infos) > maxDirEntries {
		infos = infos[:maxDirEntries]
	}

	entries := make([]FileEntry, 0, len(infos))
	for _, info := range infos {
		entries = append(entries, FileEntry{
			Name:    info.Name(),
			Size:    info.Size(),
			Mode:    info.Mode(),
			ModTime: info.ModTime(),
			IsDir:   info.IsDir(),
		})
	}
	sortEntries(entries)
	return entries, nil
}

func ReadLocalDir(path string) ([]FileEntry, error) {
	infos, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	if len(infos) > maxDirEntries {
		infos = infos[:maxDirEntries]
	}

	entries := make([]FileEntry, 0, len(infos))
	for _, info := range infos {
		fi, _ := info.Info()
		size := int64(0)
		modTime := time.Time{}
		isDir := info.IsDir()
		if fi != nil {
			size = fi.Size()
			modTime = fi.ModTime()
		}
		entries = append(entries, FileEntry{
			Name:    info.Name(),
			Size:    size,
			Mode:    info.Type(),
			ModTime: modTime,
			IsDir:   isDir,
		})
	}
	sortEntries(entries)
	return entries, nil
}

func LocalPathInfo(path string) (FileEntry, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return FileEntry{}, false, nil
	}
	if err != nil {
		return FileEntry{}, false, err
	}
	return fileEntryFromInfo(info), true, nil
}

func (s *Client) RemotePathInfo(ctx context.Context, path string) (FileEntry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := checkCanceled(ctx); err != nil {
		return FileEntry{}, false, err
	}
	info, err := s.client.Lstat(path)
	if os.IsNotExist(err) {
		return FileEntry{}, false, nil
	}
	if err != nil {
		return FileEntry{}, false, err
	}
	return fileEntryFromInfo(info), true, nil
}

func checkCanceled(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (s *Client) Download(ctx context.Context, remotePath, localPath string, progress func(int64)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := checkCanceled(ctx); err != nil {
		return err
	}

	r, err := s.client.Open(remotePath)
	if err != nil {
		return err
	}
	defer r.Close()
	stopWatchingRemote := closeOnCancel(ctx, r)
	defer stopWatchingRemote()

	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return err
	}
	w, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer w.Close()
	stopWatchingLocal := closeOnCancel(ctx, w)
	defer stopWatchingLocal()

	return copyWithContext(ctx, r, w, progress)
}

func (s *Client) Upload(ctx context.Context, localPath, remotePath string, progress func(int64)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := checkCanceled(ctx); err != nil {
		return err
	}

	r, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer r.Close()
	stopWatchingLocal := closeOnCancel(ctx, r)
	defer stopWatchingLocal()

	if err := s.client.MkdirAll(RemoteDir(remotePath)); err != nil {
		return err
	}
	w, err := s.client.Create(remotePath)
	if err != nil {
		return err
	}
	defer w.Close()
	stopWatchingRemote := closeOnCancel(ctx, w)
	defer stopWatchingRemote()

	return copyWithContext(ctx, r, w, progress)
}

func (s *Client) Preview(ctx context.Context, path string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := checkCanceled(ctx); err != nil {
		return "", err
	}

	r, err := s.client.Open(path)
	if err != nil {
		return "", err
	}
	defer r.Close()
	stopWatching := closeOnCancel(ctx, r)
	defer stopWatching()

	buf := make([]byte, 4096)
	if err := checkCanceled(ctx); err != nil {
		return "", err
	}
	n, err := r.Read(buf)
	if err != nil && err != io.EOF {
		return "", err
	}

	content := string(buf[:n])
	if !isBinary(content) {
		return content, nil
	}

	if n > 0 {
		cmd := exec.CommandContext(ctx, "file", "-")
		cmd.Stdin = strings.NewReader(string(buf[:n]))
		out, err := cmd.Output()
		if err == nil && len(out) > 0 {
			result := strings.TrimSpace(string(out))
			if idx := strings.Index(result, ": "); idx >= 0 {
				result = result[idx+2:]
			}
			return "[binary] " + result, nil
		}
	}

	return "[binary file]", nil
}

func (s *Client) Rename(ctx context.Context, oldPath, newPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := checkCanceled(ctx); err != nil {
		return err
	}
	return s.client.Rename(oldPath, newPath)
}

func isBinary(s string) bool {
	for _, r := range s {
		if r == 0 {
			return true
		}
	}
	return false
}

func (s *Client) copyRemoteToLocal(ctx context.Context, remotePath, localPath string) error {
	if err := checkCanceled(ctx); err != nil {
		return err
	}
	r, err := s.client.Open(remotePath)
	if err != nil {
		return fmt.Errorf("open remote file: %w", err)
	}
	defer r.Close()
	stopWatchingRemote := closeOnCancel(ctx, r)
	defer stopWatchingRemote()

	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("create local directory: %w", err)
	}
	w, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("create local file: %w", err)
	}
	defer w.Close()
	stopWatchingLocal := closeOnCancel(ctx, w)
	defer stopWatchingLocal()

	if err := copyWithContext(ctx, r, w, nil); err != nil {
		return fmt.Errorf("copy file data: %w", err)
	}
	return nil
}

func (s *Client) copyLocalToRemote(ctx context.Context, localPath, remotePath string) error {
	if err := checkCanceled(ctx); err != nil {
		return err
	}
	r, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local file: %w", err)
	}
	defer r.Close()
	stopWatchingLocal := closeOnCancel(ctx, r)
	defer stopWatchingLocal()

	if err := s.client.MkdirAll(RemoteDir(remotePath)); err != nil {
		return fmt.Errorf("create remote directory: %w", err)
	}
	w, err := s.client.Create(remotePath)
	if err != nil {
		return fmt.Errorf("create remote file: %w", err)
	}
	defer w.Close()
	stopWatchingRemote := closeOnCancel(ctx, w)
	defer stopWatchingRemote()

	if err := copyWithContext(ctx, r, w, nil); err != nil {
		return fmt.Errorf("copy file data: %w", err)
	}
	return nil
}

func copyWithContext(ctx context.Context, r io.Reader, w io.Writer, progress func(int64)) error {
	buf := make([]byte, 32*1024)
	var total int64
	for {
		if err := checkCanceled(ctx); err != nil {
			return err
		}
		n, readErr := r.Read(buf)
		if n > 0 {
			if err := checkCanceled(ctx); err != nil {
				return err
			}
			written, writeErr := w.Write(buf[:n])
			if writeErr != nil {
				if ctxErr := checkCanceled(ctx); ctxErr != nil {
					return ctxErr
				}
				return writeErr
			}
			if written != n {
				if ctxErr := checkCanceled(ctx); ctxErr != nil {
					return ctxErr
				}
				return io.ErrShortWrite
			}
			total += int64(n)
			if progress != nil {
				progress(total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			if ctxErr := checkCanceled(ctx); ctxErr != nil {
				return ctxErr
			}
			return readErr
		}
	}
	return checkCanceled(ctx)
}

func closeOnCancel(ctx context.Context, c io.Closer) func() {
	if ctx == nil || c == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = c.Close()
		case <-done:
		}
	}()
	return func() {
		close(done)
	}
}
