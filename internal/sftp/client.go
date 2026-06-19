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

type SkippedItem struct {
	Path   string
	Reason string
}

const maxSkippedDetails = 5

type TransferReport struct {
	Skipped      []SkippedItem
	SkippedCount int
}

type ExistingItem struct {
	Path string
	Kind string
}

const maxExistingDetails = 5

type OverwriteReport struct {
	Items []ExistingItem
	Count int
}

func (r *TransferReport) addSkipped(path string, reason interface{}) {
	r.SkippedCount++
	if len(r.Skipped) >= maxSkippedDetails {
		return
	}

	reasonText := fmt.Sprint(reason)
	if reasonText == "" {
		reasonText = "skipped"
	}
	r.Skipped = append(r.Skipped, SkippedItem{
		Path:   path,
		Reason: reasonText,
	})
}

func (r TransferReport) HasSkipped() bool {
	return r.SkippedCount > 0
}

func (r *OverwriteReport) addExisting(path, kind string) {
	r.Count++
	if len(r.Items) >= maxExistingDetails {
		return
	}
	if kind == "" {
		kind = "existing target"
	}
	r.Items = append(r.Items, ExistingItem{
		Path: path,
		Kind: kind,
	})
}

func (r OverwriteReport) HasOverwrites() bool {
	return r.Count > 0
}

func (r *OverwriteReport) AddExisting(path, kind string) {
	r.addExisting(path, kind)
}

func FileEntryKind(entry FileEntry) string {
	if entry.IsDir {
		return "directory"
	}
	if entry.Mode.IsRegular() {
		return "file"
	}
	switch {
	case entry.Mode&fs.ModeSymlink != 0:
		return "symbolic link"
	case entry.Mode&fs.ModeSocket != 0:
		return "socket"
	case entry.Mode&fs.ModeDevice != 0:
		return "device file"
	case entry.Mode&fs.ModeNamedPipe != 0:
		return "named pipe"
	case entry.Mode&fs.ModeIrregular != 0:
		return "irregular file"
	default:
		return "existing target"
	}
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

func fileEntryFromInfo(info os.FileInfo) FileEntry {
	if info == nil {
		return FileEntry{}
	}
	return FileEntry{
		Name:    info.Name(),
		Size:    info.Size(),
		Mode:    info.Mode(),
		ModTime: info.ModTime(),
		IsDir:   info.IsDir(),
	}
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

func (s *Client) DownloadDir(ctx context.Context, sourceRemoteDir, localDir string, progress func(done, total int64, file string)) (TransferReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var report TransferReport
	if err := checkCanceled(ctx); err != nil {
		return report, err
	}
	if !allowLocalDirectoryTarget(localDir, &report) {
		return report, nil
	}

	totalSize, err := s.remoteRegularFileTotal(ctx, sourceRemoteDir, map[string]struct{}{})
	if err != nil {
		return report, err
	}

	var done int64
	err = s.downloadRemoteEntry(ctx, sourceRemoteDir, sourceRemoteDir, localDir, map[string]struct{}{}, &report, &done, totalSize, progress)
	if err != nil {
		return report, err
	}
	return report, nil
}

func (s *Client) UploadDirOverwriteReport(ctx context.Context, localDir, targetRemoteDir string) (OverwriteReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var report OverwriteReport
	if err := checkCanceled(ctx); err != nil {
		return report, err
	}
	s.addRemoteDirectoryTargetConflict(ctx, targetRemoteDir, &report)
	err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if cancelErr := checkCanceled(ctx); cancelErr != nil {
			return cancelErr
		}
		if err != nil {
			if info != nil && info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !isSyncableRegularFile(info) {
			return nil
		}
		rel, err := filepath.Rel(localDir, path)
		if err != nil {
			return nil
		}
		remotePath := JoinRemotePath(targetRemoteDir, rel)
		kind, exists, err := s.remoteExistingKind(ctx, remotePath)
		if err != nil {
			report.addExisting(remotePath, "unable to verify: "+err.Error())
			return nil
		}
		if exists {
			report.addExisting(remotePath, kind)
		}
		return nil
	})
	return report, err
}

func (s *Client) DownloadDirOverwriteReport(ctx context.Context, sourceRemoteDir, localDir string) (OverwriteReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var report OverwriteReport
	if err := checkCanceled(ctx); err != nil {
		return report, err
	}
	addLocalDirectoryTargetConflict(localDir, &report)
	err := s.downloadDirOverwriteReport(ctx, sourceRemoteDir, sourceRemoteDir, localDir, map[string]struct{}{}, &report)
	return report, err
}

func (s *Client) addRemoteDirectoryTargetConflict(ctx context.Context, targetRemoteDir string, report *OverwriteReport) {
	info, exists, err := s.remoteExistingInfo(ctx, targetRemoteDir)
	if err != nil {
		report.addExisting(targetRemoteDir, "unable to verify: "+err.Error())
		return
	}
	if exists && !info.IsDir() {
		report.addExisting(targetRemoteDir, existingItemKind(info))
	}
}

func addLocalDirectoryTargetConflict(localDir string, report *OverwriteReport) {
	info, err := os.Lstat(localDir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		report.addExisting(localDir, "unable to verify: "+err.Error())
		return
	}
	if !info.IsDir() {
		report.addExisting(localDir, existingItemKind(info))
	}
}

func (s *Client) downloadDirOverwriteReport(ctx context.Context, rootRemoteDir, remotePath, localRoot string, seen map[string]struct{}, report *OverwriteReport) error {
	if err := checkCanceled(ctx); err != nil {
		return err
	}
	info, err := s.client.Lstat(remotePath)
	if err != nil {
		return nil
	}
	if info.IsDir() {
		key := pathpkg.Clean(remotePath)
		if _, ok := seen[key]; ok {
			return nil
		}
		seen[key] = struct{}{}
		entries, err := s.client.ReadDirContext(ctx, remotePath)
		if err != nil {
			return nil
		}
		for _, entry := range entries {
			name := entry.Name()
			if name == "." || name == ".." {
				continue
			}
			if err := s.downloadDirOverwriteReport(ctx, rootRemoteDir, JoinRemotePath(remotePath, name), localRoot, seen, report); err != nil {
				return err
			}
		}
		return nil
	}
	if !isSyncableRegularFile(info) {
		return nil
	}
	rel, err := RemoteRel(rootRemoteDir, remotePath)
	if err != nil {
		return nil
	}
	localPath := filepath.Join(localRoot, rel)
	kind, exists, err := localExistingKind(localPath)
	if err != nil {
		report.addExisting(localPath, "unable to verify: "+err.Error())
		return nil
	}
	if exists {
		report.addExisting(localPath, kind)
	}
	return nil
}

func (s *Client) remoteExistingKind(ctx context.Context, path string) (string, bool, error) {
	info, exists, err := s.remoteExistingInfo(ctx, path)
	if err != nil || !exists {
		return "", exists, err
	}
	return existingItemKind(info), true, nil
}

func (s *Client) remoteExistingInfo(ctx context.Context, path string) (os.FileInfo, bool, error) {
	if err := checkCanceled(ctx); err != nil {
		return nil, false, err
	}
	info, err := s.client.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return info, true, nil
}

func (s *Client) remoteRegularFileTotal(ctx context.Context, remotePath string, seen map[string]struct{}) (int64, error) {
	if err := checkCanceled(ctx); err != nil {
		return 0, err
	}
	info, err := s.client.Lstat(remotePath)
	if err != nil {
		return 0, nil
	}
	if info.IsDir() {
		key := pathpkg.Clean(remotePath)
		if _, ok := seen[key]; ok {
			return 0, nil
		}
		seen[key] = struct{}{}
		entries, err := s.client.ReadDirContext(ctx, remotePath)
		if err != nil {
			return 0, nil
		}
		var total int64
		for _, entry := range entries {
			name := entry.Name()
			if name == "." || name == ".." {
				continue
			}
			childTotal, err := s.remoteRegularFileTotal(ctx, JoinRemotePath(remotePath, name), seen)
			if err != nil {
				return 0, err
			}
			total += childTotal
		}
		return total, nil
	}
	if !isSyncableRegularFile(info) {
		return 0, nil
	}
	return info.Size(), nil
}

func (s *Client) downloadRemoteEntry(ctx context.Context, rootRemoteDir, remotePath, localRoot string, seen map[string]struct{}, report *TransferReport, done *int64, total int64, progress func(done, total int64, file string)) error {
	if err := checkCanceled(ctx); err != nil {
		return err
	}
	info, err := s.client.Lstat(remotePath)
	if err != nil {
		report.addSkipped(remotePath, err)
		return nil
	}

	rel, err := RemoteRel(rootRemoteDir, remotePath)
	if err != nil {
		report.addSkipped(remotePath, err)
		return nil
	}
	localPath := filepath.Join(localRoot, rel)

	if info.IsDir() {
		key := pathpkg.Clean(remotePath)
		if _, ok := seen[key]; ok {
			report.addSkipped(remotePath, "directory already visited")
			return nil
		}
		seen[key] = struct{}{}
		if err := os.MkdirAll(localPath, 0755); err != nil {
			report.addSkipped(remotePath, fmt.Sprintf("create local directory %s: %v", localPath, err))
			return nil
		}
		entries, err := s.client.ReadDirContext(ctx, remotePath)
		if err != nil {
			report.addSkipped(remotePath, err)
			return nil
		}
		for _, entry := range entries {
			name := entry.Name()
			if name == "." || name == ".." {
				continue
			}
			if err := s.downloadRemoteEntry(ctx, rootRemoteDir, JoinRemotePath(remotePath, name), localRoot, seen, report, done, total, progress); err != nil {
				return err
			}
		}
		return nil
	}

	if !isSyncableRegularFile(info) {
		report.addSkipped(remotePath, nonRegularFileReason(info))
		return nil
	}
	if err := s.copyRemoteToLocal(ctx, remotePath, localPath); err != nil {
		if ctxErr := checkCanceled(ctx); ctxErr != nil {
			return ctxErr
		}
		report.addSkipped(remotePath, fmt.Sprintf("copy to %s: %v", localPath, err))
		return nil
	}
	*done += info.Size()
	if progress != nil {
		progress(*done, total, RemoteBase(remotePath))
	}
	return nil
}

func (s *Client) UploadDir(ctx context.Context, localDir, targetRemoteDir string, progress func(done, total int64, file string)) (TransferReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var report TransferReport
	if err := checkCanceled(ctx); err != nil {
		return report, err
	}
	if !s.allowRemoteDirectoryTarget(ctx, targetRemoteDir, &report) {
		return report, nil
	}

	var totalSize int64
	if err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if cancelErr := checkCanceled(ctx); cancelErr != nil {
			return cancelErr
		}
		if err != nil {
			if info != nil && info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !isSyncableRegularFile(info) {
			return nil
		}
		totalSize += info.Size()
		return nil
	}); err != nil {
		return report, err
	}

	if err := s.client.MkdirAll(targetRemoteDir); err != nil {
		report.addSkipped(targetRemoteDir, fmt.Sprintf("create remote directory: %v", err))
		return report, nil
	}

	var done int64
	err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if cancelErr := checkCanceled(ctx); cancelErr != nil {
			return cancelErr
		}
		if err != nil {
			report.addSkipped(path, err)
			if info != nil && info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(localDir, path)
		if err != nil {
			report.addSkipped(path, err)
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		remotePath := JoinRemotePath(targetRemoteDir, rel)

		if info.IsDir() {
			if err := s.client.MkdirAll(remotePath); err != nil {
				report.addSkipped(path, fmt.Sprintf("create remote directory %s: %v", remotePath, err))
				return filepath.SkipDir
			}
			return nil
		}
		if !isSyncableRegularFile(info) {
			report.addSkipped(path, nonRegularFileReason(info))
			return nil
		}

		if err := s.copyLocalToRemote(ctx, path, remotePath); err != nil {
			if ctxErr := checkCanceled(ctx); ctxErr != nil {
				return ctxErr
			}
			report.addSkipped(path, fmt.Sprintf("copy to %s: %v", remotePath, err))
			return nil
		}
		done += info.Size()
		if progress != nil {
			progress(done, totalSize, filepath.Base(path))
		}
		return nil
	})
	return report, err
}

func allowLocalDirectoryTarget(localDir string, report *TransferReport) bool {
	info, err := os.Lstat(localDir)
	if os.IsNotExist(err) {
		return true
	}
	if err != nil {
		report.addSkipped(localDir, fmt.Sprintf("verify local target: %v", err))
		return false
	}
	if info.IsDir() {
		return true
	}
	report.addSkipped(localDir, fmt.Sprintf("target exists as %s", existingItemKind(info)))
	return false
}

func (s *Client) allowRemoteDirectoryTarget(ctx context.Context, targetRemoteDir string, report *TransferReport) bool {
	info, exists, err := s.remoteExistingInfo(ctx, targetRemoteDir)
	if err != nil {
		report.addSkipped(targetRemoteDir, fmt.Sprintf("verify remote target: %v", err))
		return false
	}
	if !exists || info.IsDir() {
		return true
	}
	report.addSkipped(targetRemoteDir, fmt.Sprintf("target exists as %s", existingItemKind(info)))
	return false
}

func isSyncableRegularFile(info os.FileInfo) bool {
	if info == nil || info.IsDir() {
		return false
	}
	return info.Mode().IsRegular()
}

func nonRegularFileReason(info os.FileInfo) string {
	if info == nil {
		return "missing file info"
	}
	mode := info.Mode()
	switch {
	case info.IsDir():
		return "directory"
	case mode&os.ModeSymlink != 0:
		return "symbolic link"
	case mode&os.ModeSocket != 0:
		return "socket"
	case mode&os.ModeDevice != 0:
		return "device file"
	case mode&os.ModeNamedPipe != 0:
		return "named pipe"
	case mode&os.ModeIrregular != 0:
		return "irregular file"
	default:
		return fmt.Sprintf("non-regular file (%s)", mode.Type())
	}
}

func localExistingKind(path string) (string, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return existingItemKind(info), true, nil
}

func existingItemKind(info os.FileInfo) string {
	if info == nil {
		return "existing target"
	}
	if info.Mode().IsRegular() {
		return "file"
	}
	return nonRegularFileReason(info)
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
