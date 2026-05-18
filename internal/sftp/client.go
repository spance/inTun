package sftp

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	sftp "github.com/pkg/sftp"
)

type FileEntry struct {
	Name    string
	Size    int64
	Mode    fs.FileMode
	ModTime time.Time
	IsDir   bool
}

type ProgressInfo struct {
	mu        sync.RWMutex
	Done      int64
	Total     int64
	File      string
	FileIndex int
	FileCount int
	Speed     int64
	Active    bool
}

type ProgressSnapshot struct {
	Done      int64
	Total     int64
	File      string
	FileIndex int
	FileCount int
	Speed     int64
	Active    bool
}

func NewProgressInfo(file string, total int64) *ProgressInfo {
	return &ProgressInfo{
		File:   file,
		Total:  total,
		Active: true,
	}
}

func (p *ProgressInfo) SetDone(done int64) {
	p.mu.Lock()
	p.Done = done
	p.mu.Unlock()
}

func (p *ProgressInfo) SetRecursive(done, total int64, file string) {
	p.mu.Lock()
	p.Done = done
	p.Total = total
	p.File = file
	p.mu.Unlock()
}

func (p *ProgressInfo) SetSpeed(speed int64) {
	p.mu.Lock()
	p.Speed = speed
	p.mu.Unlock()
}

func (p *ProgressInfo) SetActive(active bool) {
	p.mu.Lock()
	p.Active = active
	p.mu.Unlock()
}

func (p *ProgressInfo) Snapshot() ProgressSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return ProgressSnapshot{
		Done:      p.Done,
		Total:     p.Total,
		File:      p.File,
		FileIndex: p.FileIndex,
		FileCount: p.FileCount,
		Speed:     p.Speed,
		Active:    p.Active,
	}
}

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

func sortEntries(entries []FileEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}

func JoinRemotePath(base string, parts ...string) string {
	path := strings.TrimRight(base, "/")
	for _, part := range parts {
		part = filepath.ToSlash(part)
		part = strings.ReplaceAll(part, "\\", "/")
		part = strings.Trim(part, "/")
		if part == "" || part == "." {
			continue
		}
		if path == "" {
			path = "/" + part
		} else {
			path += "/" + part
		}
	}
	if path == "" {
		return "/"
	}
	return path
}

func RemoteDir(remotePath string) string {
	dir := pathpkg.Dir(remotePath)
	if dir == "." {
		return "/"
	}
	return dir
}

func RemoteBase(remotePath string) string {
	return pathpkg.Base(remotePath)
}

func RemoteRel(base, target string) (string, error) {
	base = pathpkg.Clean(base)
	target = pathpkg.Clean(target)
	if base == "." || base == "/" {
		return strings.TrimPrefix(target, "/"), nil
	}
	if target == base {
		return ".", nil
	}
	prefix := strings.TrimRight(base, "/") + "/"
	if !strings.HasPrefix(target, prefix) {
		return "", fmt.Errorf("%q is not under %q", target, base)
	}
	return strings.TrimPrefix(target, prefix), nil
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

func (s *Client) DownloadDir(ctx context.Context, sourceRemoteDir, localDir string, progress func(done, total int64, file string)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := checkCanceled(ctx); err != nil {
		return err
	}

	var totalSize int64
	walker := s.client.Walk(sourceRemoteDir)
	for walker.Step() {
		if err := checkCanceled(ctx); err != nil {
			return err
		}
		if err := walker.Err(); err != nil {
			return err
		}
		stat := walker.Stat()
		if stat != nil && !stat.IsDir() {
			totalSize += stat.Size()
		}
	}

	walker = s.client.Walk(sourceRemoteDir)
	var done int64
	for walker.Step() {
		if err := checkCanceled(ctx); err != nil {
			return err
		}
		if err := walker.Err(); err != nil {
			return err
		}
		path := walker.Path()
		stat := walker.Stat()
		if stat == nil {
			continue
		}

		rel, err := RemoteRel(sourceRemoteDir, path)
		if err != nil {
			return err
		}
		localPath := filepath.Join(localDir, rel)

		if stat.IsDir() {
			if err := os.MkdirAll(localPath, 0755); err != nil {
				return err
			}
			continue
		}

		if err := s.copyRemoteToLocal(ctx, path, localPath); err != nil {
			return err
		}
		done += stat.Size()
		if progress != nil {
			progress(done, totalSize, RemoteBase(path))
		}
	}
	return nil
}

func (s *Client) UploadDir(ctx context.Context, localDir, targetRemoteDir string, progress func(done, total int64, file string)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := checkCanceled(ctx); err != nil {
		return err
	}

	var totalSize int64
	if err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if cancelErr := checkCanceled(ctx); cancelErr != nil {
			return cancelErr
		}
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		totalSize += info.Size()
		return nil
	}); err != nil {
		return err
	}

	if err := s.client.MkdirAll(targetRemoteDir); err != nil {
		return err
	}

	var done int64
	err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if cancelErr := checkCanceled(ctx); cancelErr != nil {
			return cancelErr
		}
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(localDir, path)
		if err != nil {
			return err
		}
		remotePath := JoinRemotePath(targetRemoteDir, rel)

		if info.IsDir() {
			return s.client.MkdirAll(remotePath)
		}

		if err := s.copyLocalToRemote(ctx, path, remotePath); err != nil {
			return err
		}
		done += info.Size()
		if progress != nil {
			progress(done, totalSize, filepath.Base(path))
		}
		return nil
	})
	return err
}

func (s *Client) copyRemoteToLocal(ctx context.Context, remotePath, localPath string) error {
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

	return copyWithContext(ctx, r, w, nil)
}

func (s *Client) copyLocalToRemote(ctx context.Context, localPath, remotePath string) error {
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

	return copyWithContext(ctx, r, w, nil)
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
