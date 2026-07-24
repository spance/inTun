package sftp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestClientRemoteFSReadCloseRename(t *testing.T) {
	remote := newFakeRemoteFS(map[string]fakeRemoteEntry{
		"/remote":        fakeRemoteDir(),
		"/remote/b.txt":  fakeRemoteFile("b"),
		"/remote/a.txt":  fakeRemoteFile("a"),
		"/remote/nested": fakeRemoteDir(),
	})
	client := newClientWithFS(remote)

	entries, err := client.ReadRemoteDir(context.Background(), "/remote")
	if err != nil {
		t.Fatal(err)
	}
	if got := entryNames(entries); strings.Join(got, ",") != "nested,a.txt,b.txt" {
		t.Fatalf("remote entries = %v, want sorted dirs-first order", got)
	}

	if err := client.Rename(context.Background(), "/remote/a.txt", "/remote/c.txt"); err != nil {
		t.Fatal(err)
	}
	if _, exists := remote.entries["/remote/a.txt"]; exists {
		t.Fatal("old remote path should be removed after rename")
	}
	if _, exists := remote.entries["/remote/c.txt"]; !exists {
		t.Fatal("new remote path should exist after rename")
	}
	if err := client.Rename(context.Background(), "/remote/c.txt", "/remote/b.txt"); !errors.Is(err, ErrOverwriteConfirmationRequired) {
		t.Fatalf("rename over existing target error = %v", err)
	}
	if _, exists := remote.entries["/remote/c.txt"]; !exists {
		t.Fatal("source should remain after rejected overwrite")
	}

	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if remote.closed != 1 {
		t.Fatalf("remote close count = %d, want 1", remote.closed)
	}
}

func TestDownloadDirCopiesRegularFilesAndSkipsRemoteSpecials(t *testing.T) {
	remote := newFakeRemoteFS(map[string]fakeRemoteEntry{
		"/src":                fakeRemoteDir(),
		"/src/file.txt":       fakeRemoteFile("hello"),
		"/src/link":           fakeRemoteSymlink(),
		"/src/sub":            fakeRemoteDir(),
		"/src/sub/nested.txt": fakeRemoteFile("nested"),
	})
	client := newClientWithFS(remote)
	localRoot := filepath.Join(t.TempDir(), "download")
	var progress []progressSample

	report, err := client.DownloadDir(context.Background(), "/src", localRoot, func(done, total int64, file string) {
		progress = append(progress, progressSample{done: done, total: total, file: file})
	})
	if err != nil {
		t.Fatal(err)
	}

	assertFileContent(t, filepath.Join(localRoot, "file.txt"), "hello")
	assertFileContent(t, filepath.Join(localRoot, "sub", "nested.txt"), "nested")
	if _, err := os.Lstat(filepath.Join(localRoot, "link")); !os.IsNotExist(err) {
		t.Fatalf("remote symlink should not be materialized locally, stat error=%v", err)
	}
	if report.SkippedCount != 1 || !strings.Contains(report.Skipped[0].Reason, "symbolic link") {
		t.Fatalf("download report = %#v, want one symbolic link skip", report)
	}
	last := progress[len(progress)-1]
	if last.done != 11 || last.total != 11 {
		t.Fatalf("last progress = %#v, want 11/11 total bytes", last)
	}
}

func TestUploadDirCopiesRegularFilesAndSkipsLocalSymlink(t *testing.T) {
	localRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(localRoot, "file.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(localRoot, "sub")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "nested.txt"), []byte("nested"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(localRoot, "file.txt"), filepath.Join(localRoot, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	remote := newFakeRemoteFS(nil)
	client := newClientWithFS(remote)
	var progress []progressSample

	report, err := client.UploadDir(context.Background(), localRoot, "/dst", func(done, total int64, file string) {
		progress = append(progress, progressSample{done: done, total: total, file: file})
	})
	if err != nil {
		t.Fatal(err)
	}

	assertRemoteContent(t, remote, "/dst/file.txt", "hello")
	assertRemoteContent(t, remote, "/dst/sub/nested.txt", "nested")
	if _, exists := remote.entries["/dst/link"]; exists {
		t.Fatal("local symlink should not be uploaded")
	}
	if report.SkippedCount != 1 || !strings.Contains(report.Skipped[0].Reason, "symbolic link") {
		t.Fatalf("upload report = %#v, want one symbolic link skip", report)
	}
	last := progress[len(progress)-1]
	if last.done != 11 || last.total != 11 {
		t.Fatalf("last progress = %#v, want 11/11 total bytes", last)
	}
}

func TestUploadDirRejectsRemoteFileTarget(t *testing.T) {
	localRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(localRoot, "file.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	remote := newFakeRemoteFS(map[string]fakeRemoteEntry{
		"/dst": fakeRemoteFile("existing"),
	})
	client := newClientWithFS(remote)

	report, err := client.UploadDir(context.Background(), localRoot, "/dst", nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.SkippedCount != 1 || !strings.Contains(report.Skipped[0].Reason, "target exists as file") {
		t.Fatalf("upload target report = %#v, want file target skip", report)
	}
	assertRemoteContent(t, remote, "/dst", "existing")
	if _, exists := remote.entries["/dst/file.txt"]; exists {
		t.Fatal("upload should not write children under a non-directory target")
	}
}

func TestDirectoryOverwriteReportsWithRemoteFS(t *testing.T) {
	localRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(localRoot, "file.txt"), []byte("local"), 0644); err != nil {
		t.Fatal(err)
	}
	remote := newFakeRemoteFS(map[string]fakeRemoteEntry{
		"/dst":              fakeRemoteDir(),
		"/dst/file.txt":     fakeRemoteFile("remote"),
		"/src":              fakeRemoteDir(),
		"/src/existing.txt": fakeRemoteFile("remote"),
	})
	client := newClientWithFS(remote)

	uploadReport, err := client.UploadDirOverwriteReport(context.Background(), localRoot, "/dst")
	if err != nil {
		t.Fatal(err)
	}
	if uploadReport.Count != 1 || uploadReport.Items[0].Path != "/dst/file.txt" {
		t.Fatalf("upload overwrite report = %#v, want /dst/file.txt", uploadReport)
	}

	localTarget := filepath.Join(t.TempDir(), "download")
	if err := os.Mkdir(localTarget, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localTarget, "existing.txt"), []byte("local"), 0644); err != nil {
		t.Fatal(err)
	}
	downloadReport, err := client.DownloadDirOverwriteReport(context.Background(), "/src", localTarget)
	if err != nil {
		t.Fatal(err)
	}
	if downloadReport.Count != 1 || downloadReport.Items[0].Path != filepath.Join(localTarget, "existing.txt") {
		t.Fatalf("download overwrite report = %#v, want local existing file", downloadReport)
	}
}

type progressSample struct {
	done  int64
	total int64
	file  string
}

type fakeRemoteFS struct {
	entries      map[string]fakeRemoteEntry
	closed       int
	posixRename  bool
	lstatCalls   int
	readDirCalls int
}

type fakeRemoteEntry struct {
	info remoteTestInfo
	data []byte
}

type remoteTestInfo struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
	dir     bool
}

func (i remoteTestInfo) Name() string       { return i.name }
func (i remoteTestInfo) Size() int64        { return i.size }
func (i remoteTestInfo) Mode() fs.FileMode  { return i.mode }
func (i remoteTestInfo) ModTime() time.Time { return i.modTime }
func (i remoteTestInfo) IsDir() bool        { return i.dir }
func (i remoteTestInfo) Sys() interface{}   { return nil }

func newFakeRemoteFS(entries map[string]fakeRemoteEntry) *fakeRemoteFS {
	fs := &fakeRemoteFS{entries: make(map[string]fakeRemoteEntry), posixRename: true}
	for remotePath, entry := range entries {
		clean := cleanFakeRemotePath(remotePath)
		if entry.info.name == "" {
			entry.info.name = path.Base(clean)
		}
		fs.entries[clean] = entry
	}
	return fs
}

func fakeRemoteDir() fakeRemoteEntry {
	return fakeRemoteEntry{info: remoteTestInfo{mode: os.ModeDir | 0755, dir: true}}
}

func fakeRemoteFile(content string) fakeRemoteEntry {
	return fakeRemoteEntry{
		info: remoteTestInfo{size: int64(len(content)), mode: 0644},
		data: []byte(content),
	}
}

func fakeRemoteSymlink() fakeRemoteEntry {
	return fakeRemoteEntry{info: remoteTestInfo{mode: os.ModeSymlink}}
}

func (f *fakeRemoteFS) Close() error {
	f.closed++
	return nil
}

func (f *fakeRemoteFS) Rename(oldPath, newPath string) error {
	oldPath = cleanFakeRemotePath(oldPath)
	newPath = cleanFakeRemotePath(newPath)
	entry, ok := f.entries[oldPath]
	if !ok {
		return fakePathError("rename", oldPath, os.ErrNotExist)
	}
	delete(f.entries, oldPath)
	entry.info.name = path.Base(newPath)
	f.entries[newPath] = entry
	return nil
}

func (f *fakeRemoteFS) PosixRename(oldPath, newPath string) error {
	return f.Rename(oldPath, newPath)
}

func (f *fakeRemoteFS) HasExtension(name string) (string, bool) {
	return "1", f.posixRename && name == "posix-rename@openssh.com"
}

func (f *fakeRemoteFS) Remove(remotePath string) error {
	remotePath = cleanFakeRemotePath(remotePath)
	if _, ok := f.entries[remotePath]; !ok {
		return fakePathError("remove", remotePath, os.ErrNotExist)
	}
	delete(f.entries, remotePath)
	return nil
}

func (f *fakeRemoteFS) Open(remotePath string) (io.ReadCloser, error) {
	remotePath = cleanFakeRemotePath(remotePath)
	entry, ok := f.entries[remotePath]
	if !ok || entry.info.IsDir() {
		return nil, fakePathError("open", remotePath, os.ErrNotExist)
	}
	return io.NopCloser(bytes.NewReader(entry.data)), nil
}

func (f *fakeRemoteFS) Create(remotePath string) (io.WriteCloser, error) {
	remotePath = cleanFakeRemotePath(remotePath)
	return &fakeRemoteWriter{fs: f, path: remotePath}, nil
}

func (f *fakeRemoteFS) MkdirAll(remotePath string) error {
	remotePath = cleanFakeRemotePath(remotePath)
	if remotePath == "/" {
		f.entries["/"] = fakeRemoteDir()
		return nil
	}
	current := "/"
	for _, part := range strings.Split(strings.Trim(remotePath, "/"), "/") {
		current = JoinRemotePath(current, part)
		entry := fakeRemoteDir()
		entry.info.name = path.Base(current)
		f.entries[current] = entry
	}
	return nil
}

func (f *fakeRemoteFS) Lstat(remotePath string) (os.FileInfo, error) {
	f.lstatCalls++
	remotePath = cleanFakeRemotePath(remotePath)
	entry, ok := f.entries[remotePath]
	if !ok {
		return nil, fakePathError("lstat", remotePath, os.ErrNotExist)
	}
	return entry.info, nil
}

func (f *fakeRemoteFS) ReadDirContext(ctx context.Context, remotePath string) ([]os.FileInfo, error) {
	f.readDirCalls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	remotePath = cleanFakeRemotePath(remotePath)
	entry, ok := f.entries[remotePath]
	if !ok || !entry.info.IsDir() {
		return nil, fakePathError("readdir", remotePath, os.ErrNotExist)
	}
	var infos []os.FileInfo
	for candidatePath, candidate := range f.entries {
		if candidatePath == remotePath {
			continue
		}
		if path.Dir(candidatePath) == remotePath {
			infos = append(infos, candidate.info)
		}
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name() < infos[j].Name()
	})
	return infos, nil
}

type fakeRemoteWriter struct {
	bytes.Buffer
	fs   *fakeRemoteFS
	path string
}

func (w *fakeRemoteWriter) Close() error {
	data := append([]byte(nil), w.Bytes()...)
	w.fs.entries[w.path] = fakeRemoteEntry{
		info: remoteTestInfo{name: path.Base(w.path), size: int64(len(data)), mode: 0644},
		data: data,
	}
	return nil
}

func cleanFakeRemotePath(remotePath string) string {
	if remotePath == "" {
		return "/"
	}
	clean := path.Clean(remotePath)
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	return clean
}

func fakePathError(op, remotePath string, err error) error {
	return &os.PathError{Op: op, Path: remotePath, Err: err}
}

func assertRemoteContent(t *testing.T, remote *fakeRemoteFS, remotePath, want string) {
	t.Helper()

	entry, exists := remote.entries[cleanFakeRemotePath(remotePath)]
	if !exists {
		t.Fatalf("remote path %s does not exist", remotePath)
	}
	if string(entry.data) != want {
		t.Fatalf("remote content for %s = %q, want %q", remotePath, string(entry.data), want)
	}
}

func assertFileContent(t *testing.T, localPath, want string) {
	t.Helper()

	data, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("local content for %s = %q, want %q", localPath, string(data), want)
	}
}
