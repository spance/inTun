package tui

import (
	"context"
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
	return m.preflightSingleSync(pending)
}

func (m Model) sftpDoSingleSync(pending sftpPendingSync) (tea.Model, tea.Cmd) {
	if pending.source == "" || pending.target == "" {
		m.setStatusMsg("No file selected")
		return m, nil
	}
	m.sftpTransferring = true
	m.sftpDirection = "↓"
	if pending.focus == 0 {
		m.sftpDirection = "↑"
	}
	m.sftpProgress = sftp.NewProgressInfo(pending.name, pending.size)
	m.sftpPrevDone = 0
	m.sftpTransferID++
	transferID := m.sftpTransferID
	ctx, cancel := context.WithCancel(m.sftpContext())
	m.sftpCancel = cancel
	client := m.sftpClient
	progress := m.sftpProgress
	return m, func() tea.Msg {
		var err error
		direction := "download"
		if pending.focus == 0 {
			direction = "upload"
			err = client.UploadWithOptions(ctx, pending.source, pending.target, sftp.TransferOptions{AllowOverwrite: pending.allowOverwrite}, func(n int64) {
				progress.SetDone(n)
			})
		} else {
			err = client.DownloadWithOptions(ctx, pending.source, pending.target, sftp.TransferOptions{AllowOverwrite: pending.allowOverwrite}, func(n int64) {
				progress.SetDone(n)
			})
		}
		return sftpTransferResult{id: transferID, err: err, source: pending.source, target: pending.target, direction: direction}
	}
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

	return m.planDirectorySync(m.sftpFocus, src, dst)
}

func (m Model) sftpDoRecursive() (tea.Model, tea.Cmd) {
	if m.sftpPendingDirPlan == nil {
		return m, nil
	}
	plan := *m.sftpPendingDirPlan
	m.sftpPendingDirPlan = nil
	m.sftpTransferring = true
	m.sftpDirection = "⇡"
	direction := "upload"
	if plan.Direction == sftp.SyncDownload {
		m.sftpDirection = "⇣"
		direction = "download"
	}
	m.sftpProgress = sftp.NewProgressInfo(filepath.Base(plan.SourceRoot), plan.TotalBytes)
	m.sftpPrevDone = 0
	m.sftpTransferID++
	transferID := m.sftpTransferID
	ctx, cancel := context.WithCancel(m.sftpContext())
	m.sftpCancel = cancel
	client := m.sftpClient
	progress := m.sftpProgress
	return m, func() tea.Msg {
		report, err := client.ExecuteSyncPlan(ctx, plan, sftp.TransferOptions{ApprovedOverwrites: plan.Overwrites.ApprovedPaths()}, func(done, total int64, file string) {
			progress.SetRecursive(done, total, file)
		})
		return sftpTransferResult{id: transferID, err: err, source: plan.SourceRoot, target: plan.TargetRoot, direction: direction, report: report}
	}
}
