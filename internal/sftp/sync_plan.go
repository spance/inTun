package sftp

import (
	"context"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
)

type SyncDirection int

const (
	SyncUpload SyncDirection = iota
	SyncDownload
)

type SyncFile struct {
	Source string
	Target string
	Size   int64
}

type SyncDirectory struct {
	Source string
	Target string
}

type SyncPlan struct {
	Direction   SyncDirection
	SourceRoot  string
	TargetRoot  string
	Directories []SyncDirectory
	Files       []SyncFile
	TotalBytes  int64
	Overwrites  OverwriteReport
	Report      TransferReport
}

func (s *Client) PlanUploadDir(ctx context.Context, localDir, targetRemoteDir string) (SyncPlan, error) {
	plan := SyncPlan{Direction: SyncUpload, SourceRoot: localDir, TargetRoot: targetRemoteDir}
	_, done, err := s.beginOperation(ctx)
	if err != nil {
		return plan, err
	}
	defer done()

	err = walkLocalTree(ctx, localDir, func(localPath string, info os.FileInfo) error {
		rel, relErr := filepath.Rel(localDir, localPath)
		if relErr != nil {
			plan.Report.addSkipped(localPath, relErr)
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		remotePath := targetRemoteDir
		if rel != "." {
			remotePath = JoinRemotePath(targetRemoteDir, rel)
		}

		if info.IsDir() {
			plan.Directories = append(plan.Directories, SyncDirectory{Source: localPath, Target: remotePath})
			s.inspectRemoteDirectoryTarget(ctx, remotePath, &plan.Overwrites)
			return nil
		}
		if !isSyncableRegularFile(info) {
			plan.Report.addSkipped(localPath, nonRegularFileReason(info))
			return nil
		}

		plan.Files = append(plan.Files, SyncFile{Source: localPath, Target: remotePath, Size: info.Size()})
		plan.TotalBytes += info.Size()
		s.inspectRemoteFileTarget(ctx, remotePath, &plan.Overwrites)
		return nil
	}, transferReportLocalWalkError(&plan.Report))
	return plan, err
}

func (s *Client) PlanDownloadDir(ctx context.Context, sourceRemoteDir, localDir string) (SyncPlan, error) {
	plan := SyncPlan{Direction: SyncDownload, SourceRoot: sourceRemoteDir, TargetRoot: localDir}
	_, done, err := s.beginOperation(ctx)
	if err != nil {
		return plan, err
	}
	defer done()

	err = s.planRemoteEntry(ctx, sourceRemoteDir, sourceRemoteDir, localDir, map[string]struct{}{}, &plan)
	return plan, err
}

func (s *Client) ExecuteSyncPlan(ctx context.Context, plan SyncPlan, options TransferOptions, progress func(done, total int64, file string)) (TransferReport, error) {
	report := plan.Report
	_, operationDone, err := s.beginOperation(ctx)
	if err != nil {
		return report, err
	}
	defer operationDone()

	blockedDirectories := make([]string, 0)
	for _, directory := range plan.Directories {
		if err := checkCanceled(ctx); err != nil {
			return report, err
		}
		if plan.Direction == SyncUpload {
			info, exists, inspectErr := s.remoteExistingInfo(ctx, directory.Target)
			if inspectErr != nil {
				report.addSkipped(directory.Source, fmt.Sprintf("inspect remote directory %s: %v", directory.Target, inspectErr))
				blockedDirectories = append(blockedDirectories, directory.Target)
				continue
			}
			if exists && !info.IsDir() {
				report.addSkipped(directory.Source, fmt.Sprintf("target exists as %s", existingItemKind(info)))
				blockedDirectories = append(blockedDirectories, directory.Target)
				continue
			}
			if err := s.client.MkdirAll(directory.Target); err != nil {
				report.addSkipped(directory.Source, fmt.Sprintf("create remote directory %s: %v", directory.Target, err))
				blockedDirectories = append(blockedDirectories, directory.Target)
			}
			continue
		}
		info, inspectErr := os.Lstat(directory.Target)
		if inspectErr == nil && !info.IsDir() {
			report.addSkipped(directory.Source, fmt.Sprintf("target exists as %s", existingItemKind(info)))
			blockedDirectories = append(blockedDirectories, directory.Target)
			continue
		}
		if inspectErr != nil && !os.IsNotExist(inspectErr) {
			report.addSkipped(directory.Source, fmt.Sprintf("inspect local directory %s: %v", directory.Target, inspectErr))
			blockedDirectories = append(blockedDirectories, directory.Target)
			continue
		}
		if err := os.MkdirAll(directory.Target, 0755); err != nil {
			report.addSkipped(directory.Source, fmt.Sprintf("create local directory %s: %v", directory.Target, err))
			blockedDirectories = append(blockedDirectories, directory.Target)
		}
	}

	var completed int64
	for _, file := range plan.Files {
		if err := checkCanceled(ctx); err != nil {
			return report, err
		}
		if targetUnderBlockedDirectory(plan.Direction, file.Target, blockedDirectories) {
			continue
		}
		var copyErr error
		if plan.Direction == SyncUpload {
			copyErr = s.copyLocalToRemote(ctx, file.Source, file.Target, options.allowsOverwrite(file.Target))
		} else {
			copyErr = s.copyRemoteToLocal(ctx, file.Source, file.Target, options.allowsOverwrite(file.Target))
		}
		if copyErr != nil {
			if ctxErr := checkCanceled(ctx); ctxErr != nil {
				return report, ctxErr
			}
			report.addSkipped(file.Source, fmt.Sprintf("copy to %s: %v", file.Target, copyErr))
			continue
		}
		completed += file.Size
		if progress != nil {
			progress(completed, plan.TotalBytes, syncFileBase(plan.Direction, file))
		}
	}
	return report, nil
}

func targetUnderBlockedDirectory(direction SyncDirection, target string, blocked []string) bool {
	for _, directory := range blocked {
		if direction == SyncUpload {
			target = pathpkg.Clean(target)
			directory = pathpkg.Clean(directory)
			if target == directory || len(target) > len(directory) && target[:len(directory)] == directory && target[len(directory)] == '/' {
				return true
			}
			continue
		}
		target = filepath.Clean(target)
		directory = filepath.Clean(directory)
		rel, err := filepath.Rel(directory, target)
		if err == nil && rel != ".." && rel != "." && !filepath.IsAbs(rel) {
			return true
		}
		if target == directory {
			return true
		}
	}
	return false
}

func (s *Client) planRemoteEntry(ctx context.Context, rootRemoteDir, remotePath, localRoot string, seen map[string]struct{}, plan *SyncPlan) error {
	if err := checkCanceled(ctx); err != nil {
		return err
	}
	info, err := s.client.Lstat(remotePath)
	if err != nil {
		plan.Report.addSkipped(remotePath, fmt.Sprintf("inspect remote entry: %v", err))
		return nil
	}
	rel, err := RemoteRel(rootRemoteDir, remotePath)
	if err != nil {
		plan.Report.addSkipped(remotePath, err)
		return nil
	}
	localPath := localRoot
	if rel != "." && rel != "" {
		localPath = filepath.Join(localRoot, rel)
	}

	if info.IsDir() {
		key := pathpkg.Clean(remotePath)
		if _, ok := seen[key]; ok {
			plan.Report.addSkipped(remotePath, "directory already visited")
			return nil
		}
		seen[key] = struct{}{}
		plan.Directories = append(plan.Directories, SyncDirectory{Source: remotePath, Target: localPath})
		inspectLocalDirectoryTarget(localPath, &plan.Overwrites)
		entries, readErr := s.client.ReadDirContext(ctx, remotePath)
		if readErr != nil {
			plan.Report.addSkipped(remotePath, fmt.Sprintf("read remote directory: %v", readErr))
			return nil
		}
		for _, entry := range entries {
			if entry.Name() == "." || entry.Name() == ".." {
				continue
			}
			if err := s.planRemoteEntry(ctx, rootRemoteDir, JoinRemotePath(remotePath, entry.Name()), localRoot, seen, plan); err != nil {
				return err
			}
		}
		return nil
	}
	if !isSyncableRegularFile(info) {
		plan.Report.addSkipped(remotePath, nonRegularFileReason(info))
		return nil
	}

	plan.Files = append(plan.Files, SyncFile{Source: remotePath, Target: localPath, Size: info.Size()})
	plan.TotalBytes += info.Size()
	inspectLocalFileTarget(localPath, &plan.Overwrites)
	return nil
}

func (s *Client) inspectRemoteDirectoryTarget(ctx context.Context, target string, report *OverwriteReport) {
	info, exists, err := s.remoteExistingInfo(ctx, target)
	if err != nil {
		report.addExisting(target, "unable to verify: "+err.Error())
		return
	}
	if exists && !info.IsDir() {
		report.addExisting(target, existingItemKind(info))
	}
}

func (s *Client) inspectRemoteFileTarget(ctx context.Context, target string, report *OverwriteReport) {
	kind, exists, err := s.remoteExistingKind(ctx, target)
	if err != nil {
		report.addExisting(target, "unable to verify: "+err.Error())
		return
	}
	if exists {
		report.addExisting(target, kind)
	}
}

func inspectLocalDirectoryTarget(target string, report *OverwriteReport) {
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		report.addExisting(target, "unable to verify: "+err.Error())
		return
	}
	if !info.IsDir() {
		report.addExisting(target, existingItemKind(info))
	}
}

func inspectLocalFileTarget(target string, report *OverwriteReport) {
	kind, exists, err := localExistingKind(target)
	if err != nil {
		report.addExisting(target, "unable to verify: "+err.Error())
		return
	}
	if exists {
		report.addExisting(target, kind)
	}
}

func syncFileBase(direction SyncDirection, file SyncFile) string {
	if direction == SyncUpload {
		return filepath.Base(file.Source)
	}
	return RemoteBase(file.Source)
}
