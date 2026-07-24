package tui

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/spance/intun/internal/sftp"
)

func (m Model) handleSFTPKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	keys := defaultSFTPKeyMap

	if m.sftpRenaming {
		switch msg.String() {
		case "enter":
			return m.sftpConfirmRename()
		case "esc":
			m.sftpRenaming = false
			m.sftpRenameInput = ""
		case "backspace":
			runes := []rune(m.sftpRenameInput)
			if len(runes) > 0 {
				m.sftpRenameInput = string(runes[:len(runes)-1])
			}
		default:
			if text := msg.Key().Text; text != "" {
				m.sftpRenameInput += text
			}
		}
		return m, nil
	}

	if m.sftpOverwriteConfirm {
		switch {
		case matchKey(msg, keys.Confirm):
			pending := m.sftpPendingSync
			pending.allowOverwrite = true
			m.sftpOverwriteConfirm = false
			m.sftpOverwriteConfirmMsg = ""
			m.sftpPendingSync = sftpPendingSync{}
			return m.sftpDoSingleSync(pending)
		case matchKey(msg, keys.Cancel):
			m.sftpOverwriteConfirm = false
			m.sftpOverwriteConfirmMsg = ""
			m.sftpPendingSync = sftpPendingSync{}
		}
		return m, nil
	}

	if m.sftpSyncConfirm {
		switch {
		case matchKey(msg, keys.Confirm):
			m.sftpSyncConfirm = false
			m.sftpSyncConfirmMsg = ""
			return m.sftpDoRecursive()
		case matchKey(msg, keys.Cancel):
			m.sftpSyncConfirm = false
			m.sftpSyncConfirmMsg = ""
			m.sftpPendingDirPlan = nil
		}
		return m, nil
	}

	if m.sftpPreviewing {
		switch {
		case matchKey(msg, keys.Close):
			m.sftpPreviewing = false
			m.sftpPreview = ""
		}
		return m, nil
	}

	if m.sftpLoading && !matchKey(msg, keys.Close) {
		m.setStatusMsg(m.sftpLoadingLabel + "...")
		return m, nil
	}

	switch {
	case matchKey(msg, keys.Close):
		if m.sftpOperationCancel != nil {
			m.sftpOperationCancel()
			m.sftpOperationCancel = nil
		}
		m.sftpOperationID++
		if m.sftpCancel != nil {
			m.sftpCancel()
			m.sftpCancel = nil
		}
		m.sftpTransferID++
		client := m.sftpClient
		m.sftpClient = nil
		m.sftpTransferring = false
		m.sftpLoading = false
		m.sftpLoadingLabel = ""
		m.sftpOverwriteConfirm = false
		m.sftpOverwriteConfirmMsg = ""
		m.sftpPendingSync = sftpPendingSync{}
		m.sftpPendingDirPlan = nil
		m.screen = ScreenMain
		if client == nil {
			return m, nil
		}
		return m, func() tea.Msg {
			_ = client.Close()
			return nil
		}
	case matchKey(msg, keys.Switch):
		if m.sftpFocus == 0 {
			m.sftpFocus = 1
		} else {
			m.sftpFocus = 0
		}
	case matchKey(msg, keys.Up):
		if m.sftpCursor[m.sftpFocus] > 0 {
			m.sftpCursor[m.sftpFocus]--
		}
	case matchKey(msg, keys.Down):
		files := m.currentSFTPFiles()
		if m.sftpCursor[m.sftpFocus] < len(files) {
			m.sftpCursor[m.sftpFocus]++
		}
	case matchKey(msg, keys.PageUp):
		visibleHeight := m.sftpListVisibleItems()
		cur := &m.sftpCursor[m.sftpFocus]
		*cur -= visibleHeight
		if *cur < 0 {
			*cur = 0
		}
	case matchKey(msg, keys.PageDown):
		files := m.currentSFTPFiles()
		visibleHeight := m.sftpListVisibleItems()
		maxCur := len(files)
		cur := &m.sftpCursor[m.sftpFocus]
		*cur += visibleHeight
		if *cur > maxCur {
			*cur = maxCur
		}
	case matchKey(msg, keys.Open):
		return m.sftpEnterDir()
	case matchKey(msg, keys.Sync):
		if m.sftpTransferring {
			m.setStatusMsg("Wait for transfer to complete")
			return m, nil
		}
		return m.sftpStartSync()
	case matchKey(msg, keys.SyncDir):
		if m.sftpTransferring {
			m.setStatusMsg("Wait for transfer to complete")
			return m, nil
		}
		return m.sftpStartRecursiveConfirm()
	case matchKey(msg, keys.Preview):
		if m.sftpTransferring {
			m.setStatusMsg("Wait for transfer to complete")
			return m, nil
		}
		return m.sftpPreviewFile()
	case matchKey(msg, keys.Rename):
		if !m.sftpTransferring {
			files := m.currentSFTPFiles()
			cursor := m.sftpCursor[m.sftpFocus]
			if cursor == 0 || cursor > len(files) {
				m.setStatusMsg("No file selected")
				return m, nil
			}
			m.sftpRenaming = true
			m.sftpRenameInput = files[cursor-1].Name
		} else {
			m.setStatusMsg("Wait for transfer to complete")
		}
	}
	return m.normalizeSFTPScroll(), nil
}

func (m Model) sftpEnterDir() (tea.Model, tea.Cmd) {
	files := m.currentSFTPFiles()
	cursor := m.sftpCursor[m.sftpFocus]
	var target string

	if cursor == 0 {
		if m.sftpFocus == 0 {
			target = filepath.Dir(m.sftpLocalDir)
		} else {
			target = sftp.RemoteDir(m.sftpRemoteDir)
		}
	} else {
		idx := cursor - 1
		if idx >= len(files) || !files[idx].IsDir {
			m.setStatusMsg("Use [v] to preview, [s] to sync")
			return m, nil
		}
		if m.sftpFocus == 0 {
			target = filepath.Join(m.sftpLocalDir, files[idx].Name)
		} else {
			target = sftp.JoinRemotePath(m.sftpRemoteDir, files[idx].Name)
		}
	}

	return m.sftpNavigateTo(target)
}

func (m Model) sftpNavigateTo(path string) (tea.Model, tea.Cmd) {
	return m.navigateSFTP(m.sftpFocus, path)
}
