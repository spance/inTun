package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/spance/intun/internal/sftp"
)

type sftpPreviewResultMsg struct {
	operationID int
	content     string
	err         error
}

type sftpRenameResultMsg struct {
	operationID int
	err         error
}

type sftpRefreshResultMsg struct {
	operationID int
	localFiles  []sftp.FileEntry
	remoteFiles []sftp.FileEntry
	localErr    error
	remoteErr   error
}

func (m Model) previewSFTPFile(focus int, path string) (Model, tea.Cmd) {
	m, operationID, ctx := m.beginSFTPOperation("Loading preview")
	client := m.sftpClient
	return m, func() tea.Msg {
		result := sftpPreviewResultMsg{operationID: operationID}
		if focus == 0 {
			if err := ctx.Err(); err != nil {
				result.err = err
				return result
			}
			data, err := readPreviewBytes(path, 4096)
			if err != nil {
				result.err = err
				return result
			}
			result.content = string(data)
			if isBinaryContent(result.content) {
				result.content = "[binary file]"
			}
		} else if client == nil {
			result.err = fmt.Errorf("SFTP client is not connected")
		} else {
			result.content, result.err = client.Preview(ctx, path)
		}
		return result
	}
}

func (m Model) handleSFTPPreviewResult(msg sftpPreviewResultMsg) Model {
	var current bool
	m, current = m.finishSFTPOperation(msg.operationID)
	if !current {
		return m
	}
	if msg.err != nil {
		m.sftpPreview = fmt.Sprintf("Error: %v", msg.err)
	} else {
		m.sftpPreview = msg.content
	}
	m.sftpPreviewing = true
	return m
}

func (m Model) renameSFTPEntry(focus int, oldPath, newPath string) (Model, tea.Cmd) {
	m, operationID, ctx := m.beginSFTPOperation("Renaming")
	client := m.sftpClient
	return m, func() tea.Msg {
		result := sftpRenameResultMsg{operationID: operationID}
		if err := ctx.Err(); err != nil {
			result.err = err
			return result
		}
		if focus == 0 {
			result.err = renameLocalNoReplace(oldPath, newPath)
		} else if client == nil {
			result.err = fmt.Errorf("SFTP client is not connected")
		} else {
			result.err = client.Rename(ctx, oldPath, newPath)
		}
		return result
	}
}

func (m Model) handleSFTPRenameResult(msg sftpRenameResultMsg) (Model, tea.Cmd) {
	var current bool
	m, current = m.finishSFTPOperation(msg.operationID)
	if !current {
		return m, nil
	}
	if msg.err != nil {
		m.setStatusMsg(fmt.Sprintf("Rename failed: %v", msg.err))
		return m, nil
	}
	return m.refreshSFTPFilesCmd()
}

func (m Model) refreshSFTPFilesCmd() (Model, tea.Cmd) {
	m, operationID, ctx := m.beginSFTPOperation("Refreshing")
	client := m.sftpClient
	localDir := m.sftpLocalDir
	remoteDir := m.sftpRemoteDir
	return m, func() tea.Msg {
		result := sftpRefreshResultMsg{operationID: operationID}
		result.localFiles, result.localErr = sftp.ReadLocalDir(localDir)
		if client == nil {
			result.remoteErr = fmt.Errorf("SFTP client is not connected")
		} else {
			result.remoteFiles, result.remoteErr = client.ReadRemoteDir(ctx, remoteDir)
		}
		return result
	}
}

func (m Model) handleSFTPRefreshResult(msg sftpRefreshResultMsg) Model {
	var current bool
	m, current = m.finishSFTPOperation(msg.operationID)
	if !current {
		return m
	}
	if msg.localErr == nil {
		m.sftpLocalFiles = msg.localFiles
	}
	if msg.remoteErr == nil {
		m.sftpRemoteFiles = msg.remoteFiles
	}
	switch {
	case msg.localErr != nil && msg.remoteErr != nil:
		m.setStatusConfirm(fmt.Sprintf("Refresh failed\nLocal: %v\nRemote: %v", msg.localErr, msg.remoteErr))
	case msg.localErr != nil:
		m.setStatusConfirm(fmt.Sprintf("Refresh failed\nLocal: %v", msg.localErr))
	case msg.remoteErr != nil:
		m.setStatusConfirm(fmt.Sprintf("Refresh failed\nRemote: %v", msg.remoteErr))
	}
	return m.normalizeSFTPScroll()
}
