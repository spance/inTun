package sftp

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDownloadRequiresOverwriteConfirmationAndCommitsAtomically(t *testing.T) {
	remote := newFakeRemoteFS(map[string]fakeRemoteEntry{
		"/remote/file.txt": fakeRemoteFile("new-content"),
	})
	client := newClientWithFS(remote)
	target := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(target, []byte("old-content"), 0600); err != nil {
		t.Fatal(err)
	}

	err := client.Download(context.Background(), "/remote/file.txt", target, nil)
	if !errors.Is(err, ErrOverwriteConfirmationRequired) {
		t.Fatalf("Download error = %v", err)
	}
	assertAtomicFileContent(t, target, "old-content")

	err = client.DownloadWithOptions(context.Background(), "/remote/file.txt", target, TransferOptions{AllowOverwrite: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertAtomicFileContent(t, target, "new-content")
	if matches, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".*.intun-*")); len(matches) != 0 {
		t.Fatalf("temporary local files remain: %v", matches)
	}
}

func TestUploadRequiresOverwriteConfirmationAndAtomicRemoteReplace(t *testing.T) {
	remote := newFakeRemoteFS(map[string]fakeRemoteEntry{
		"/remote":          fakeRemoteDir(),
		"/remote/file.txt": fakeRemoteFile("old-content"),
	})
	client := newClientWithFS(remote)
	source := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(source, []byte("new-content"), 0600); err != nil {
		t.Fatal(err)
	}

	err := client.Upload(context.Background(), source, "/remote/file.txt", nil)
	if !errors.Is(err, ErrOverwriteConfirmationRequired) {
		t.Fatalf("Upload error = %v", err)
	}
	if got := string(remote.entries["/remote/file.txt"].data); got != "old-content" {
		t.Fatalf("remote content = %q", got)
	}

	err = client.UploadWithOptions(context.Background(), source, "/remote/file.txt", TransferOptions{AllowOverwrite: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(remote.entries["/remote/file.txt"].data); got != "new-content" {
		t.Fatalf("remote content = %q", got)
	}
	for remotePath := range remote.entries {
		if strings.Contains(remotePath, ".intun-") {
			t.Fatalf("temporary remote file remains: %s", remotePath)
		}
	}
}

func TestUploadRefusesUnsafeRemoteOverwriteWithoutPosixRename(t *testing.T) {
	remote := newFakeRemoteFS(map[string]fakeRemoteEntry{
		"/remote":          fakeRemoteDir(),
		"/remote/file.txt": fakeRemoteFile("old-content"),
	})
	remote.posixRename = false
	client := newClientWithFS(remote)
	source := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(source, []byte("new-content"), 0600); err != nil {
		t.Fatal(err)
	}

	err := client.UploadWithOptions(context.Background(), source, "/remote/file.txt", TransferOptions{AllowOverwrite: true}, nil)
	if !errors.Is(err, ErrAtomicRemoteReplaceUnsupported) {
		t.Fatalf("Upload error = %v", err)
	}
	if got := string(remote.entries["/remote/file.txt"].data); got != "old-content" {
		t.Fatalf("remote content = %q", got)
	}
}

func TestExecuteSyncPlanDoesNotTraverseRemoteTreeAgain(t *testing.T) {
	remote := newFakeRemoteFS(map[string]fakeRemoteEntry{
		"/remote":          fakeRemoteDir(),
		"/remote/file.txt": fakeRemoteFile("content"),
	})
	client := newClientWithFS(remote)
	plan, err := client.PlanDownloadDir(context.Background(), "/remote", filepath.Join(t.TempDir(), "download"))
	if err != nil {
		t.Fatal(err)
	}
	readDirCalls := remote.readDirCalls
	if _, err := client.ExecuteSyncPlan(context.Background(), plan, TransferOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	if remote.readDirCalls != readDirCalls {
		t.Fatalf("ExecuteSyncPlan traversed remote tree again: before=%d after=%d", readDirCalls, remote.readDirCalls)
	}
}

func TestExecuteSyncPlanOnlyOverwritesTargetsApprovedByPlan(t *testing.T) {
	remote := newFakeRemoteFS(map[string]fakeRemoteEntry{
		"/remote":       fakeRemoteDir(),
		"/remote/a.txt": fakeRemoteFile("new-a"),
		"/remote/b.txt": fakeRemoteFile("new-b"),
	})
	client := newClientWithFS(remote)
	localDir := t.TempDir()
	aPath := filepath.Join(localDir, "a.txt")
	bPath := filepath.Join(localDir, "b.txt")
	if err := os.WriteFile(aPath, []byte("old-a"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := client.PlanDownloadDir(context.Background(), "/remote", localDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bPath, []byte("appeared-after-confirmation"), 0600); err != nil {
		t.Fatal(err)
	}

	report, err := client.ExecuteSyncPlan(context.Background(), plan, TransferOptions{
		ApprovedOverwrites: plan.Overwrites.ApprovedPaths(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertAtomicFileContent(t, aPath, "new-a")
	assertAtomicFileContent(t, bPath, "appeared-after-confirmation")
	if !report.HasSkipped() {
		t.Fatal("new overwrite risk should be skipped and reported")
	}
}

func TestInstallLocalFileNeverReplacesExistingTarget(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.tmp")
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(source, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := installLocalFile(source, target); err == nil {
		t.Fatal("exclusive install should fail when target exists")
	}
	assertAtomicFileContent(t, target, "old")
}

func TestCloseInterruptsBlockedOperation(t *testing.T) {
	remote := newBlockingRemoteFS()
	client := newClientWithFS(remote)
	target := filepath.Join(t.TempDir(), "target")
	downloadDone := make(chan error, 1)
	go func() {
		downloadDone <- client.Download(context.Background(), "/blocked", target, nil)
	}()
	<-remote.openStarted

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.CloseContext(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-downloadDone:
	case <-time.After(time.Second):
		t.Fatal("blocked SFTP operation did not exit after Close")
	}
}

func assertAtomicFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s content = %q, want %q", path, data, want)
	}
}

type blockingRemoteFS struct {
	openStarted chan struct{}
	closed      chan struct{}
	closeOnce   sync.Once
}

func newBlockingRemoteFS() *blockingRemoteFS {
	return &blockingRemoteFS{openStarted: make(chan struct{}), closed: make(chan struct{})}
}

func (f *blockingRemoteFS) Close() error {
	f.closeOnce.Do(func() { close(f.closed) })
	return nil
}

func (f *blockingRemoteFS) Open(string) (io.ReadCloser, error) {
	close(f.openStarted)
	<-f.closed
	return nil, errors.New("closed")
}

func (f *blockingRemoteFS) Rename(string, string) error           { return nil }
func (f *blockingRemoteFS) PosixRename(string, string) error      { return nil }
func (f *blockingRemoteFS) HasExtension(string) (string, bool)    { return "", false }
func (f *blockingRemoteFS) Create(string) (io.WriteCloser, error) { return nil, errors.New("blocked") }
func (f *blockingRemoteFS) Remove(string) error                   { return nil }
func (f *blockingRemoteFS) MkdirAll(string) error                 { return nil }
func (f *blockingRemoteFS) Lstat(string) (os.FileInfo, error)     { return nil, os.ErrNotExist }
func (f *blockingRemoteFS) ReadDirContext(context.Context, string) ([]os.FileInfo, error) {
	return nil, errors.New("blocked")
}
