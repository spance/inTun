package sftp

import (
	"context"
	"os"
	"time"
)

func (s *Client) ReadRemoteDir(ctx context.Context, path string) ([]FileEntry, error) {
	_, done, err := s.beginOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer done()

	infos, err := s.client.ReadDirContext(ctx, path)
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
	_, done, err := s.beginOperation(ctx)
	if err != nil {
		return FileEntry{}, false, err
	}
	defer done()
	info, err := s.client.Lstat(path)
	if os.IsNotExist(err) {
		return FileEntry{}, false, nil
	}
	if err != nil {
		return FileEntry{}, false, err
	}
	return fileEntryFromInfo(info), true, nil
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
