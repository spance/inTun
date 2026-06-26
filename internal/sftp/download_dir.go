package sftp

import (
	"context"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
)

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
