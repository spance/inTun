package sftp

import (
	"context"
	"os"
	"path/filepath"
)

type localWalkEntryFunc func(path string, info os.FileInfo) error
type localWalkErrorFunc func(path string, info os.FileInfo, err error) error

func walkLocalTree(ctx context.Context, root string, onEntry localWalkEntryFunc, onError localWalkErrorFunc) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if cancelErr := checkCanceled(ctx); cancelErr != nil {
			return cancelErr
		}
		if err != nil {
			if onError != nil {
				return onError(path, info, err)
			}
			return skipLocalDirOnError(info)
		}
		if onEntry == nil {
			return nil
		}
		return onEntry(path, info)
	})
}

func skipLocalDirOnError(info os.FileInfo) error {
	if info != nil && info.IsDir() {
		return filepath.SkipDir
	}
	return nil
}

func ignoreLocalWalkError(_ string, info os.FileInfo, _ error) error {
	return skipLocalDirOnError(info)
}

func transferReportLocalWalkError(report *TransferReport) localWalkErrorFunc {
	return func(path string, info os.FileInfo, err error) error {
		report.addSkipped(path, err)
		return skipLocalDirOnError(info)
	}
}
