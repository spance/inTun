package sftp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestReadLocalDir(t *testing.T) {
	tmpDir := t.TempDir()
	os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("hello"), 0644)
	os.WriteFile(filepath.Join(tmpDir, ".hidden"), []byte("hidden"), 0644)

	entries, err := ReadLocalDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadLocalDir failed: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}

	found := map[string]bool{}
	for _, e := range entries {
		found[e.Name] = true
		if e.Name == "subdir" && !e.IsDir {
			t.Error("subdir should be a directory")
		}
		if e.Name == "file.txt" && e.IsDir {
			t.Error("file.txt should not be a directory")
		}
	}
	if !found["subdir"] || !found["file.txt"] || !found[".hidden"] {
		t.Error("missing expected entries")
	}
}

type shortWriter struct {
	bytes.Buffer
}

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return w.Buffer.Write(p[:len(p)-1])
}

func TestCopyWithContextDetectsShortWrite(t *testing.T) {
	src := bytes.NewReader([]byte("hello"))
	var dst shortWriter

	err := copyWithContext(context.Background(), src, &dst, nil)
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("copyWithContext error = %v, want io.ErrShortWrite", err)
	}
}

func TestSortEntries(t *testing.T) {
	entries := []FileEntry{
		{Name: "b.txt", IsDir: false},
		{Name: "adir", IsDir: true},
		{Name: "zdir", IsDir: true},
		{Name: "a.txt", IsDir: false},
	}
	sortEntries(entries)

	if entries[0].Name != "adir" || entries[1].Name != "zdir" {
		t.Errorf("directories should come first, got order: %v", entryNames(entries))
	}
	if entries[2].Name != "a.txt" || entries[3].Name != "b.txt" {
		t.Errorf("files should be sorted alphabetically, got order: %v", entryNames(entries))
	}
}

func entryNames(entries []FileEntry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return names
}

func TestReadLocalDirEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	entries, err := ReadLocalDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadLocalDir failed: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("empty dir should have 0 entries, got %d", len(entries))
	}
}

func TestReadLocalDirRename(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "old.txt"), []byte("data"), 0644)
	os.Rename(filepath.Join(tmpDir, "old.txt"), filepath.Join(tmpDir, "new.txt"))
	entries, err := ReadLocalDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadLocalDir failed: %v", err)
	}
	found := map[string]bool{}
	for _, e := range entries {
		found[e.Name] = true
	}
	if found["old.txt"] {
		t.Error("old.txt should not exist after rename")
	}
	if !found["new.txt"] {
		t.Error("new.txt should exist after rename")
	}
}

func TestIsBinary(t *testing.T) {
	if isBinary("hello world") {
		t.Error("plain text should not be binary")
	}
	if !isBinary("hello\x00world") {
		t.Error("string with null byte should be binary")
	}
}

func TestProgressInfoSnapshot(t *testing.T) {
	progress := NewProgressInfo("initial.txt", 100)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := int64(0); i < 100; i++ {
			progress.SetRecursive(i, 100, "current.txt")
		}
	}()
	go func() {
		defer wg.Done()
		for i := int64(0); i < 100; i++ {
			progress.SetSpeed(i)
			_ = progress.Snapshot()
		}
	}()
	wg.Wait()

	snapshot := progress.Snapshot()
	if snapshot.File != "current.txt" {
		t.Fatalf("File = %q, want current.txt", snapshot.File)
	}
	if snapshot.Total != 100 {
		t.Fatalf("Total = %d, want 100", snapshot.Total)
	}
	if !snapshot.Active {
		t.Fatal("progress should remain active")
	}
}

func TestJoinRemotePath(t *testing.T) {
	tests := []struct {
		base  string
		parts []string
		want  string
	}{
		{base: "/", parts: []string{"file.txt"}, want: "/file.txt"},
		{base: "/home/user", parts: []string{"file.txt"}, want: "/home/user/file.txt"},
		{base: "/home/user/", parts: []string{"/dir/", "file.txt"}, want: "/home/user/dir/file.txt"},
		{base: "/home/user", parts: []string{"dir\\file.txt"}, want: "/home/user/dir/file.txt"},
		{base: "", parts: []string{"dir", "file.txt"}, want: "/dir/file.txt"},
		{base: "/", parts: nil, want: "/"},
	}

	for _, tt := range tests {
		got := JoinRemotePath(tt.base, tt.parts...)
		if got != tt.want {
			t.Fatalf("JoinRemotePath(%q, %v) = %q, want %q", tt.base, tt.parts, got, tt.want)
		}
	}
}

func TestRemotePathHelpers(t *testing.T) {
	if got := RemoteDir("/home/user/project"); got != "/home/user" {
		t.Fatalf("RemoteDir = %q, want /home/user", got)
	}
	if got := RemoteDir("/"); got != "/" {
		t.Fatalf("RemoteDir root = %q, want /", got)
	}
	if got := RemoteBase("/home/user/file.txt"); got != "file.txt" {
		t.Fatalf("RemoteBase = %q, want file.txt", got)
	}

	rel, err := RemoteRel("/home/user", "/home/user/dir/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if rel != "dir/file.txt" {
		t.Fatalf("RemoteRel = %q, want dir/file.txt", rel)
	}
	rel, err = RemoteRel("/", "/home/user/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if rel != "home/user/file.txt" {
		t.Fatalf("RemoteRel root = %q, want home/user/file.txt", rel)
	}
	if _, err := RemoteRel("/home/user", "/other/file.txt"); err == nil {
		t.Fatal("RemoteRel should reject paths outside base")
	}
}

func TestCheckCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := checkCanceled(ctx); err != context.Canceled {
		t.Fatalf("checkCanceled = %v, want context.Canceled", err)
	}
	if err := checkCanceled(context.Background()); err != nil {
		t.Fatalf("uncancelled context returned %v", err)
	}
}

func TestCopyWithContextCancelsBetweenChunks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := bytes.NewReader(bytes.Repeat([]byte("x"), 96*1024))
	var dst bytes.Buffer
	err := copyWithContext(ctx, src, &dst, func(int64) {
		cancel()
	})
	if err != context.Canceled {
		t.Fatalf("copyWithContext error = %v, want context.Canceled", err)
	}
	if dst.Len() == 0 {
		t.Fatal("expected first chunk to be copied before cancellation")
	}
	if dst.Len() >= 96*1024 {
		t.Fatalf("copy should stop before full input, copied %d", dst.Len())
	}
}
