package sftp

import (
	"context"
	"fmt"
	"os"
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
