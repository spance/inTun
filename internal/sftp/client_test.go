package sftp

import (
	"os"
	"path/filepath"
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
