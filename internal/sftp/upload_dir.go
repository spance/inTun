package sftp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

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
	if err := walkLocalTree(ctx, localDir, func(_ string, info os.FileInfo) error {
		if !isSyncableRegularFile(info) {
			return nil
		}
		totalSize += info.Size()
		return nil
	}, ignoreLocalWalkError); err != nil {
		return report, err
	}

	if err := s.client.MkdirAll(targetRemoteDir); err != nil {
		report.addSkipped(targetRemoteDir, fmt.Sprintf("create remote directory: %v", err))
		return report, nil
	}

	var done int64
	err := walkLocalTree(ctx, localDir, func(path string, info os.FileInfo) error {
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
	}, transferReportLocalWalkError(&report))
	return report, err
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
