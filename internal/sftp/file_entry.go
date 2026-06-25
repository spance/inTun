package sftp

import (
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"
)

type FileEntry struct {
	Name    string
	Size    int64
	Mode    fs.FileMode
	ModTime time.Time
	IsDir   bool
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
