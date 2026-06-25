package tui

import (
	"context"
	"fmt"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/spance/intun/internal/sftp"
)

func (m Model) sftpStartSync() (tea.Model, tea.Cmd) {
	if m.sftpFocus == 0 {
		files := m.sftpLocalFiles
		cursor := m.sftpCursor[0]
		if cursor == 0 || cursor > len(files) {
			m.setStatusMsg("No file selected")
			return m, nil
		}
		entry := files[cursor-1]
		if entry.IsDir {
			m.setStatusMsg("Use [r] for directory sync")
			return m, nil
		}
		localPath := filepath.Join(m.sftpLocalDir, entry.Name)
		remotePath := sftp.JoinRemotePath(m.sftpRemoteDir, entry.Name)
		pending := sftpPendingSync{focus: 0, source: localPath, target: remotePath, name: entry.Name, size: entry.Size}
		return m.sftpStartSingleSync(pending)
	}

	files := m.sftpRemoteFiles
	cursor := m.sftpCursor[1]
	if cursor == 0 || cursor > len(files) {
		m.setStatusMsg("No file selected")
		return m, nil
	}
	entry := files[cursor-1]
	if entry.IsDir {
		m.setStatusMsg("Use [r] for directory sync")
		return m, nil
	}
	remotePath := sftp.JoinRemotePath(m.sftpRemoteDir, entry.Name)
	localPath := filepath.Join(m.sftpLocalDir, entry.Name)
	pending := sftpPendingSync{focus: 1, source: remotePath, target: localPath, name: entry.Name, size: entry.Size}
	return m.sftpStartSingleSync(pending)
}

func (m Model) sftpStartSingleSync(pending sftpPendingSync) (tea.Model, tea.Cmd) {
	next, ok := m.validateSingleSyncSource(pending)
	if !ok {
		return next, nil
	}
	report, err := m.singleSyncOverwriteReport(pending)
	if err != nil {
		m.setStatusMsg(fmt.Sprintf("Cannot check overwrite risk: %v", err))
		return m, nil
	}
	if report.HasOverwrites() {
		m.sftpOverwriteConfirm = true
		m.sftpPendingSync = pending
		m.sftpOverwriteConfirmMsg = formatFileOverwriteConfirmMessage(pending.focus, pending.source, pending.target, report)
		return m, nil
	}
	return m.sftpDoSingleSync(pending)
}

func (m Model) validateSingleSyncSource(pending sftpPendingSync) (Model, bool) {
	var (
		entry  sftp.FileEntry
		exists bool
		err    error
	)
	if pending.focus == 0 {
		entry, exists, err = sftp.LocalPathInfo(pending.source)
	} else {
		entry, exists, err = m.sftpClient.RemotePathInfo(m.sftpContext(), pending.source)
	}
	if err != nil {
		m.setStatusMsg(fmt.Sprintf("Cannot inspect source: %v", err))
		return m, false
	}
	if !exists {
		m.setStatusMsg("Source no longer exists")
		return m, false
	}
	if !entry.Mode.IsRegular() {
		m.setStatusMsg(fmt.Sprintf("Skipped non-regular file: %s", sftp.FileEntryKind(entry)))
		return m, false
	}
	return m, true
}

func (m Model) singleSyncOverwriteReport(pending sftpPendingSync) (sftp.OverwriteReport, error) {
	var report sftp.OverwriteReport
	if pending.focus == 0 {
		entry, exists, err := m.sftpClient.RemotePathInfo(m.sftpContext(), pending.target)
		if err != nil {
			report.AddExisting(pending.target, "unable to verify: "+err.Error())
			return report, nil
		}
		if exists {
			report.AddExisting(pending.target, sftp.FileEntryKind(entry))
		}
		return report, nil
	}

	entry, exists, err := sftp.LocalPathInfo(pending.target)
	if err != nil {
		report.AddExisting(pending.target, "unable to verify: "+err.Error())
		return report, nil
	}
	if exists {
		report.AddExisting(pending.target, sftp.FileEntryKind(entry))
	}
	return report, nil
}

func (m Model) sftpDoSingleSync(pending sftpPendingSync) (tea.Model, tea.Cmd) {
	if pending.source == "" || pending.target == "" {
		m.setStatusMsg("No file selected")
		return m, nil
	}
	next, ok := m.validateSingleSyncSource(pending)
	if !ok {
		return next, nil
	}

	m.sftpTransferring = true
	m.sftpDirection = "↓"
	if pending.focus == 0 {
		m.sftpDirection = "↑"
	}
	m.sftpProgress = sftp.NewProgressInfo(pending.name, pending.size)
	m.sftpPrevDone = 0
	done := make(chan sftpTransferResult, 1)
	m.sftpDone = done
	m.sftpTransferID++
	transferID := m.sftpTransferID
	ctx, cancel := context.WithCancel(m.sftpContext())
	m.sftpCancel = cancel
	client := m.sftpClient
	progress := m.sftpProgress
	go func() {
		var err error
		direction := "download"
		if pending.focus == 0 {
			direction = "upload"
			err = client.Upload(ctx, pending.source, pending.target, func(n int64) {
				progress.SetDone(n)
			})
		} else {
			err = client.Download(ctx, pending.source, pending.target, func(n int64) {
				progress.SetDone(n)
			})
		}
		done <- sftpTransferResult{id: transferID, err: err, source: pending.source, target: pending.target, direction: direction}
	}()
	return m, nil
}

func (m Model) sftpStartRecursiveConfirm() (tea.Model, tea.Cmd) {
	files := m.currentSFTPFiles()
	cursor := m.sftpCursor[m.sftpFocus]
	if cursor == 0 || cursor > len(files) {
		m.setStatusMsg("No file selected")
		return m, nil
	}
	entry := files[cursor-1]
	if !entry.IsDir {
		m.setStatusMsg("Use [s] for file sync")
		return m, nil
	}

	var src, dst string
	if m.sftpFocus == 0 {
		src = filepath.Join(m.sftpLocalDir, entry.Name)
		dst = sftp.JoinRemotePath(m.sftpRemoteDir, entry.Name)
	} else {
		src = sftp.JoinRemotePath(m.sftpRemoteDir, entry.Name)
		dst = filepath.Join(m.sftpLocalDir, entry.Name)
	}

	var overwriteReport sftp.OverwriteReport
	var err error
	if m.sftpFocus == 0 {
		overwriteReport, err = m.sftpClient.UploadDirOverwriteReport(m.sftpContext(), src, dst)
	} else {
		overwriteReport, err = m.sftpClient.DownloadDirOverwriteReport(m.sftpContext(), src, dst)
	}
	if err != nil {
		m.setStatusMsg(fmt.Sprintf("Cannot check overwrite risk: %v", err))
		return m, nil
	}

	m.sftpSyncConfirm = true
	m.sftpSyncConfirmMsg = formatDirectorySyncConfirmMessage(m.sftpFocus, src, dst, overwriteReport)
	return m, nil
}

func (m Model) sftpDoRecursive() (tea.Model, tea.Cmd) {
	if m.sftpFocus == 0 {
		files := m.sftpLocalFiles
		cursor := m.sftpCursor[0]
		if cursor == 0 || cursor > len(files) {
			return m, nil
		}
		entry := files[cursor-1]
		if !entry.IsDir {
			return m, nil
		}
		localPath := filepath.Join(m.sftpLocalDir, entry.Name)
		remotePath := sftp.JoinRemotePath(m.sftpRemoteDir, entry.Name)
		m.sftpTransferring = true
		m.sftpDirection = "⇡"
		m.sftpProgress = sftp.NewProgressInfo(entry.Name, 0)
		m.sftpPrevDone = 0
		done := make(chan sftpTransferResult, 1)
		m.sftpDone = done
		m.sftpTransferID++
		transferID := m.sftpTransferID
		ctx, cancel := context.WithCancel(m.sftpContext())
		m.sftpCancel = cancel
		client := m.sftpClient
		progress := m.sftpProgress
		go func() {
			report, err := client.UploadDir(ctx, localPath, remotePath, func(done, total int64, file string) {
				progress.SetRecursive(done, total, file)
			})
			done <- sftpTransferResult{id: transferID, err: err, source: localPath, target: remotePath, direction: "upload", report: report}
		}()
		return m, nil
	}

	files := m.sftpRemoteFiles
	cursor := m.sftpCursor[1]
	if cursor == 0 || cursor > len(files) {
		return m, nil
	}
	entry := files[cursor-1]
	if !entry.IsDir {
		return m, nil
	}
	remotePath := sftp.JoinRemotePath(m.sftpRemoteDir, entry.Name)
	localPath := filepath.Join(m.sftpLocalDir, entry.Name)
	m.sftpTransferring = true
	m.sftpDirection = "⇣"
	m.sftpProgress = sftp.NewProgressInfo(entry.Name, 0)
	m.sftpPrevDone = 0
	done := make(chan sftpTransferResult, 1)
	m.sftpDone = done
	m.sftpTransferID++
	transferID := m.sftpTransferID
	ctx, cancel := context.WithCancel(m.sftpContext())
	m.sftpCancel = cancel
	client := m.sftpClient
	progress := m.sftpProgress
	go func() {
		report, err := client.DownloadDir(ctx, remotePath, localPath, func(done, total int64, file string) {
			progress.SetRecursive(done, total, file)
		})
		done <- sftpTransferResult{id: transferID, err: err, source: remotePath, target: localPath, direction: "download", report: report}
	}()
	return m, nil
}
