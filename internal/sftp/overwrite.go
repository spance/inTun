package sftp

import (
	"context"
	"os"
	pathpkg "path"
	"path/filepath"
)

func (s *Client) UploadDirOverwriteReport(ctx context.Context, localDir, targetRemoteDir string) (OverwriteReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var report OverwriteReport
	if err := checkCanceled(ctx); err != nil {
		return report, err
	}
	s.addRemoteDirectoryTargetConflict(ctx, targetRemoteDir, &report)
	err := walkLocalTree(ctx, localDir, func(path string, info os.FileInfo) error {
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
	}, ignoreLocalWalkError)
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
