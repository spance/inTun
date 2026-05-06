package sftp

import (
	"io/fs"
	"os"
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
