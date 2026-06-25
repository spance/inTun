package sftp

import (
	"context"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
)

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
