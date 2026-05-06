package sftp

import (
	"io"
	"io/fs"
	"os"
	"os/exec"
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
	Done      int64
	Total     int64
	File      string
	FileIndex int
	FileCount int
	Speed     int64
	Active    bool
}

type Client struct {
	client *sftp.Client
	mu     sync.Mutex
}

func NewClient(c *sftp.Client) *Client {
	return &Client{client: c}
}

func (s *Client) Close() error {
	return s.client.Close()
}

func (s *Client) ReadRemoteDir(path string) ([]FileEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	infos, err := s.client.ReadDir(path)
	if err != nil {
		return nil, err
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

func (s *Client) Download(remotePath, localPath string, progress func(int64)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, err := s.client.Open(remotePath)
	if err != nil {
		return err
	}
	defer r.Close()

	w, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer w.Close()

	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			_, writeErr := w.Write(buf[:n])
			if writeErr != nil {
				return writeErr
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
			return readErr
		}
	}
	return nil
}

func (s *Client) Upload(localPath, remotePath string, progress func(int64)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer r.Close()

	w, err := s.client.Create(remotePath)
	if err != nil {
		return err
	}
	defer w.Close()

	buf := make([]byte, 32*1024)
	var total int64
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			_, writeErr := w.Write(buf[:n])
			if writeErr != nil {
				return writeErr
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
			return readErr
		}
	}
	return nil
}

func (s *Client) Preview(path string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r, err := s.client.Open(path)
	if err != nil {
		return "", err
	}
	defer r.Close()

	buf := make([]byte, 4096)
	n, err := r.Read(buf)
	if err != nil && err != io.EOF {
		return "", err
	}

	content := string(buf[:n])
	if !isBinary(content) {
		return content, nil
	}

	if n > 0 {
		cmd := exec.Command("file", "-")
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

func isBinary(s string) bool {
	for _, r := range s {
		if r == 0 {
			return true
		}
	}
	return false
}

func (s *Client) DownloadDir(remoteDir, localDir string, progress func(done, total int64, file string)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var totalSize int64
	walker := s.client.Walk(remoteDir)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			continue
		}
		stat := walker.Stat()
		if stat != nil && !stat.IsDir() {
			totalSize += stat.Size()
		}
	}

	walker = s.client.Walk(remoteDir)
	var done int64
	for walker.Step() {
		if err := walker.Err(); err != nil {
			continue
		}
		path := walker.Path()
		stat := walker.Stat()
		if stat == nil {
			continue
		}

		rel, err := filepath.Rel(remoteDir, path)
		if err != nil {
			continue
		}
		localPath := filepath.Join(localDir, rel)

		if stat.IsDir() {
			os.MkdirAll(localPath, 0755)
			continue
		}

		if err := s.copyRemoteToLocal(path, localPath); err != nil {
			return err
		}
		done += stat.Size()
		if progress != nil {
			progress(done, totalSize, filepath.Base(path))
		}
	}
	return nil
}

func (s *Client) UploadDir(localDir, remoteDir string, progress func(done, total int64, file string)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var totalSize int64
	filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		totalSize += info.Size()
		return nil
	})

	var done int64
	err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(localDir, path)
		if err != nil {
			return err
		}
		remotePath := remoteDir + "/" + rel

		if info.IsDir() {
			s.client.Mkdir(remotePath)
			return nil
		}

		if err := s.copyLocalToRemote(path, remotePath); err != nil {
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

func (s *Client) copyRemoteToLocal(remotePath, localPath string) error {
	r, err := s.client.Open(remotePath)
	if err != nil {
		return err
	}
	defer r.Close()

	os.MkdirAll(filepath.Dir(localPath), 0755)
	w, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer w.Close()

	_, err = io.Copy(w, r)
	return err
}

func (s *Client) copyLocalToRemote(localPath, remotePath string) error {
	r, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer r.Close()

	s.client.Mkdir(filepath.Dir(remotePath))
	w, err := s.client.Create(remotePath)
	if err != nil {
		return err
	}
	defer w.Close()

	_, err = io.Copy(w, r)
	return err
}
