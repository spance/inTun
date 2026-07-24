package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/spance/intun/internal/sftp"
)

type sftpSinglePreflightResultMsg struct {
	operationID int
	pending     sftpPendingSync
	overwrites  sftp.OverwriteReport
	status      string
}

type sftpPlanResultMsg struct {
	operationID int
	focus       int
	source      string
	target      string
	plan        sftp.SyncPlan
	err         error
}

func (m Model) preflightSingleSync(pending sftpPendingSync) (Model, tea.Cmd) {
	m, operationID, ctx := m.beginSFTPOperation("Checking destination")
	client := m.sftpClient
	return m, func() tea.Msg {
		result := sftpSinglePreflightResultMsg{operationID: operationID, pending: pending}
		var (
			source sftp.FileEntry
			exists bool
			err    error
		)
		if pending.focus == 0 {
			source, exists, err = sftp.LocalPathInfo(pending.source)
		} else if client == nil {
			err = fmt.Errorf("SFTP client is not connected")
		} else {
			source, exists, err = client.RemotePathInfo(ctx, pending.source)
		}
		switch {
		case err != nil:
			result.status = fmt.Sprintf("Cannot inspect source: %v", err)
			return result
		case !exists:
			result.status = "Source no longer exists"
			return result
		case !source.Mode.IsRegular():
			result.status = fmt.Sprintf("Skipped non-regular file: %s", sftp.FileEntryKind(source))
			return result
		}

		if pending.focus == 0 {
			if client == nil {
				result.status = "Cannot inspect destination: SFTP client is not connected"
				return result
			}
			target, targetExists, targetErr := client.RemotePathInfo(ctx, pending.target)
			if targetErr != nil {
				result.overwrites.AddExisting(pending.target, "unable to verify: "+targetErr.Error())
			} else if targetExists {
				result.overwrites.AddExisting(pending.target, sftp.FileEntryKind(target))
			}
		} else {
			target, targetExists, targetErr := sftp.LocalPathInfo(pending.target)
			if targetErr != nil {
				result.overwrites.AddExisting(pending.target, "unable to verify: "+targetErr.Error())
			} else if targetExists {
				result.overwrites.AddExisting(pending.target, sftp.FileEntryKind(target))
			}
		}
		return result
	}
}

func (m Model) handleSFTPSinglePreflightResult(msg sftpSinglePreflightResultMsg) (Model, tea.Cmd) {
	var current bool
	m, current = m.finishSFTPOperation(msg.operationID)
	if !current {
		return m, nil
	}
	if msg.status != "" {
		m.setStatusMsg(msg.status)
		return m, nil
	}
	if msg.overwrites.HasOverwrites() {
		m.sftpOverwriteConfirm = true
		m.sftpPendingSync = msg.pending
		m.sftpOverwriteConfirmMsg = formatFileOverwriteConfirmMessage(
			msg.pending.focus,
			msg.pending.source,
			msg.pending.target,
			msg.overwrites,
		)
		return m, nil
	}
	updated, cmd := m.sftpDoSingleSync(msg.pending)
	return updated.(Model), cmd
}

func (m Model) planDirectorySync(focus int, source, target string) (Model, tea.Cmd) {
	m, operationID, ctx := m.beginSFTPOperation("Scanning directory")
	client := m.sftpClient
	return m, func() tea.Msg {
		result := sftpPlanResultMsg{
			operationID: operationID,
			focus:       focus,
			source:      source,
			target:      target,
		}
		if client == nil {
			result.err = fmt.Errorf("SFTP client is not connected")
			return result
		}
		if focus == 0 {
			result.plan, result.err = client.PlanUploadDir(ctx, source, target)
		} else {
			result.plan, result.err = client.PlanDownloadDir(ctx, source, target)
		}
		return result
	}
}

func (m Model) handleSFTPPlanResult(msg sftpPlanResultMsg) Model {
	var current bool
	m, current = m.finishSFTPOperation(msg.operationID)
	if !current {
		return m
	}
	if msg.err != nil {
		m.setStatusMsg(fmt.Sprintf("Cannot scan directory: %v", msg.err))
		return m
	}
	m.sftpSyncConfirm = true
	m.sftpPendingDirPlan = &msg.plan
	m.sftpSyncConfirmMsg = formatDirectorySyncConfirmMessage(msg.focus, msg.source, msg.target, msg.plan.Overwrites)
	return m
}

func (m Model) handleSFTPTransferResult(msg sftpTransferResult) (Model, tea.Cmd) {
	if msg.id != m.sftpTransferID {
		return m, nil
	}
	m.sftpTransferring = false
	m.sftpCancel = nil
	if m.sftpProgress != nil {
		m.sftpProgress.SetActive(false)
	}
	if m.screen != ScreenSFTP {
		return m, nil
	}
	if msg.err != nil {
		m.setStatusConfirm(formatSFTPTransferError(msg))
		return m, nil
	}
	m.setStatusConfirm(formatSFTPTransferSuccess(msg))
	if m.sftpClient == nil {
		return m, nil
	}
	return m.refreshSFTPFilesCmd()
}
