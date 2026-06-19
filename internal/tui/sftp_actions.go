package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/spance/intun/internal/sftp"
)

const (
	sftpTopBarRows          = 1
	sftpShortcutRows        = 1
	sftpViewPaddingRows     = 1
	sftpPanelGapRows        = 1
	sftpPanelFrameRows      = 2
	sftpPanelHeaderRows     = 2
	sftpPanelRowsAroundList = sftpPanelFrameRows + sftpPanelHeaderRows
	sftpOuterChromeRows     = sftpTopBarRows + sftpShortcutRows + sftpViewPaddingRows + sftpPanelGapRows + sftpPanelRowsAroundList
	sftpMinListRows         = 5
	sftpRenameDrawerRows    = 3
	sftpTransferDrawerRows  = 4
	sftpPreviewFrameRows    = 3
)

func (m Model) handleSFTPKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.sftpRenaming {
		switch msg.String() {
		case "enter":
			return m.sftpConfirmRename()
		case "esc":
			m.sftpRenaming = false
			m.sftpRenameInput = ""
		case "backspace":
			if len(m.sftpRenameInput) > 0 {
				m.sftpRenameInput = m.sftpRenameInput[:len(m.sftpRenameInput)-1]
			}
		default:
			if len(msg.String()) == 1 && msg.String()[0] >= 32 {
				m.sftpRenameInput += msg.String()
			}
		}
		return m, nil
	}

	if m.sftpOverwriteConfirm {
		switch msg.String() {
		case "enter":
			pending := m.sftpPendingSync
			m.sftpOverwriteConfirm = false
			m.sftpOverwriteConfirmMsg = ""
			m.sftpPendingSync = sftpPendingSync{}
			return m.sftpDoSingleSync(pending)
		case "esc":
			m.sftpOverwriteConfirm = false
			m.sftpOverwriteConfirmMsg = ""
			m.sftpPendingSync = sftpPendingSync{}
		}
		return m, nil
	}

	if m.sftpSyncConfirm {
		switch msg.String() {
		case "enter":
			m.sftpSyncConfirm = false
			m.sftpSyncConfirmMsg = ""
			return m.sftpDoRecursive()
		case "esc":
			m.sftpSyncConfirm = false
			m.sftpSyncConfirmMsg = ""
		}
		return m, nil
	}

	if m.sftpPreviewing {
		switch msg.String() {
		case "esc", "q":
			m.sftpPreviewing = false
			m.sftpPreview = ""
		}
		return m, nil
	}

	switch msg.String() {
	case "q", "esc":
		if m.sftpCancel != nil {
			m.sftpCancel()
			m.sftpCancel = nil
		}
		if m.sftpClient != nil {
			m.sftpClient.Close()
			m.sftpClient = nil
		}
		m.sftpTransferring = false
		m.sftpDone = nil
		m.sftpOverwriteConfirm = false
		m.sftpOverwriteConfirmMsg = ""
		m.sftpPendingSync = sftpPendingSync{}
		m.screen = ScreenMain
		return m, nil
	case "tab":
		if m.sftpFocus == 0 {
			m.sftpFocus = 1
		} else {
			m.sftpFocus = 0
		}
	case "up", "k":
		if m.sftpCursor[m.sftpFocus] > 0 {
			m.sftpCursor[m.sftpFocus]--
		}
	case "down", "j":
		files := m.currentSFTPFiles()
		if m.sftpCursor[m.sftpFocus] < len(files) {
			m.sftpCursor[m.sftpFocus]++
		}
	case "pgup":
		visibleHeight := m.sftpListVisibleItems()
		cur := &m.sftpCursor[m.sftpFocus]
		*cur -= visibleHeight
		if *cur < 0 {
			*cur = 0
		}
	case "pgdown":
		files := m.currentSFTPFiles()
		visibleHeight := m.sftpListVisibleItems()
		maxCur := len(files)
		cur := &m.sftpCursor[m.sftpFocus]
		*cur += visibleHeight
		if *cur > maxCur {
			*cur = maxCur
		}
	case "enter":
		return m.sftpEnterDir()
	case "s":
		if m.sftpTransferring {
			m.setStatusMsg("Wait for transfer to complete")
			return m, nil
		}
		return m.sftpStartSync()
	case "r":
		if m.sftpTransferring {
			m.setStatusMsg("Wait for transfer to complete")
			return m, nil
		}
		return m.sftpStartRecursiveConfirm()
	case "v":
		if m.sftpTransferring {
			m.setStatusMsg("Wait for transfer to complete")
			return m, nil
		}
		return m.sftpPreviewFile()
	case "n":
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

func (m Model) currentSFTPFiles() []sftp.FileEntry {
	if m.sftpFocus == 0 {
		return m.sftpLocalFiles
	}
	return m.sftpRemoteFiles
}

func (m Model) sftpContext() context.Context {
	if m.cancelCtx != nil {
		return m.cancelCtx
	}
	return context.Background()
}

func (m Model) sftpListVisibleItems() int {
	visible := m.height - sftpOuterChromeRows - m.sftpDrawerHeight()
	if visible < sftpMinListRows {
		return sftpMinListRows
	}
	return visible
}

func (m Model) sftpPanelHeight() int {
	return m.sftpListVisibleItems() + sftpPanelRowsAroundList
}

func (m Model) normalizeSFTPScroll() Model {
	visibleItems := m.sftpListVisibleItems()
	for panel := 0; panel < 2; panel++ {
		totalItems := len(m.sftpLocalFiles) + 1
		if panel == 1 {
			totalItems = len(m.sftpRemoteFiles) + 1
		}
		if totalItems < 1 {
			totalItems = 1
		}

		maxCursor := totalItems - 1
		if m.sftpCursor[panel] < 0 {
			m.sftpCursor[panel] = 0
		}
		if m.sftpCursor[panel] > maxCursor {
			m.sftpCursor[panel] = maxCursor
		}
		if m.sftpScroll[panel] < 0 {
			m.sftpScroll[panel] = 0
		}
		if m.sftpCursor[panel] < m.sftpScroll[panel] {
			m.sftpScroll[panel] = m.sftpCursor[panel]
		}
		if m.sftpCursor[panel] >= m.sftpScroll[panel]+visibleItems {
			m.sftpScroll[panel] = m.sftpCursor[panel] - visibleItems + 1
		}
		maxScroll := totalItems - visibleItems
		if maxScroll < 0 {
			maxScroll = 0
		}
		if m.sftpScroll[panel] > maxScroll {
			m.sftpScroll[panel] = maxScroll
		}
	}
	return m
}

func (m Model) sftpDrawerHeight() int {
	height := 0
	if m.sftpRenaming {
		height += sftpRenameDrawerRows
	}
	if m.sftpTransferring {
		height += sftpTransferDrawerRows
	}
	if m.sftpPreviewing {
		height += m.sftpPreviewHeight() + sftpPreviewFrameRows
	}
	return height
}

func (m Model) sftpPreviewHeight() int {
	height := m.height / 4
	if height < 4 {
		height = 4
	}
	if height > 8 {
		height = 8
	}
	return height
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
	if m.sftpFocus == 0 {
		if err := m.sftpContext().Err(); err != nil {
			m.setStatusMsg("Operation cancelled")
			return m, nil
		}
		localFiles, err := sftp.ReadLocalDir(path)
		if err != nil {
			m.setStatusMsg("Cannot open directory")
			return m, nil
		}
		m.sftpLocalDir = path
		m.sftpLocalFiles = localFiles
	} else {
		remoteFiles, err := m.sftpClient.ReadRemoteDir(m.sftpContext(), path)
		if err != nil {
			m.setStatusMsg("Cannot open directory")
			return m, nil
		}
		m.sftpRemoteDir = path
		m.sftpRemoteFiles = remoteFiles
	}
	m.sftpCursor[m.sftpFocus] = 0
	m.sftpScroll[m.sftpFocus] = 0
	return m.normalizeSFTPScroll(), nil
}

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

func formatFileOverwriteConfirmMessage(focus int, src, dst string, overwriteReport sftp.OverwriteReport) string {
	direction, source, destination := syncDirectionLabels(focus)
	var b strings.Builder
	b.WriteString(direction)
	b.WriteString("\nOVERWRITE FILE")
	if warning := formatOverwriteWarning(overwriteReport); warning != "" {
		b.WriteString("\n")
		b.WriteString(warning)
	}
	b.WriteString(fmt.Sprintf("\nSOURCE: %s  %s\nDESTINATION: %s  %s", source, src, destination, dst))
	return b.String()
}

func formatDirectorySyncConfirmMessage(focus int, src, dst string, overwriteReport sftp.OverwriteReport) string {
	direction, source, destination := syncDirectionLabels(focus)
	var b strings.Builder
	b.WriteString(direction)
	if warning := formatOverwriteWarning(overwriteReport); warning != "" {
		b.WriteString("\n")
		b.WriteString(warning)
	}
	b.WriteString(fmt.Sprintf("\nSOURCE: %s  %s\nDESTINATION: %s  %s", source, src, destination, dst))
	return b.String()
}

func syncDirectionLabels(focus int) (direction, source, destination string) {
	direction = "LOCAL -> REMOTE  UPLOAD"
	source = "LOCAL"
	destination = "REMOTE"
	if focus == 1 {
		direction = "REMOTE -> LOCAL  DOWNLOAD"
		source = "REMOTE"
		destination = "LOCAL"
	}
	return direction, source, destination
}

func formatOverwriteWarning(report sftp.OverwriteReport) string {
	if !report.HasOverwrites() {
		return ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("OVERWRITE: %d existing target item(s)", report.Count))
	for _, item := range report.Items {
		b.WriteString("\n- ")
		b.WriteString(item.Path)
		b.WriteString(": ")
		b.WriteString(item.Kind)
	}
	if remaining := report.Count - len(report.Items); remaining > 0 {
		b.WriteString(fmt.Sprintf("\n- ... %d more", remaining))
	}
	return b.String()
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

func (m Model) sftpPreviewFile() (tea.Model, tea.Cmd) {
	files := m.currentSFTPFiles()
	cursor := m.sftpCursor[m.sftpFocus]
	if cursor == 0 || cursor > len(files) {
		m.setStatusMsg("No file selected")
		return m, nil
	}
	entry := files[cursor-1]
	if entry.IsDir {
		m.setStatusMsg("Cannot preview a directory")
		return m, nil
	}

	if m.sftpFocus == 0 {
		localPath := filepath.Join(m.sftpLocalDir, entry.Name)
		if err := m.sftpContext().Err(); err != nil {
			m.sftpPreview = fmt.Sprintf("Error: %v", err)
			m.sftpPreviewing = true
			return m, nil
		}
		data, err := readPreviewBytes(localPath, 4096)
		if err != nil {
			m.sftpPreview = fmt.Sprintf("Error: %v", err)
			m.sftpPreviewing = true
			return m, nil
		}
		content := string(data)
		if isBinaryContent(content) {
			content = "[binary file]"
		}
		m.sftpPreview = content
	} else {
		remotePath := sftp.JoinRemotePath(m.sftpRemoteDir, entry.Name)
		content, err := m.sftpClient.Preview(m.sftpContext(), remotePath)
		if err != nil {
			m.sftpPreview = fmt.Sprintf("Error: %v", err)
			m.sftpPreviewing = true
			return m, nil
		}
		m.sftpPreview = content
	}
	m.sftpPreviewing = true
	return m, nil
}

func (m Model) sftpConfirmRename() (tea.Model, tea.Cmd) {
	files := m.currentSFTPFiles()
	cursor := m.sftpCursor[m.sftpFocus]
	if cursor == 0 || cursor > len(files) {
		m.sftpRenaming = false
		m.sftpRenameInput = ""
		return m, nil
	}

	oldName := files[cursor-1].Name
	newName := m.sftpRenameInput
	m.sftpRenaming = false
	m.sftpRenameInput = ""

	if newName == "" || newName == oldName {
		return m, nil
	}
	if err := validateRenameName(newName); err != nil {
		m.setStatusMsg(fmt.Sprintf("Rename failed: %v", err))
		return m, nil
	}

	if m.sftpFocus == 0 {
		oldPath := filepath.Join(m.sftpLocalDir, oldName)
		newPath := filepath.Join(m.sftpLocalDir, newName)
		if err := m.sftpContext().Err(); err != nil {
			m.setStatusMsg(fmt.Sprintf("Rename failed: %v", err))
			return m, nil
		}
		if err := os.Rename(oldPath, newPath); err != nil {
			m.setStatusMsg(fmt.Sprintf("Rename failed: %v", err))
			return m, nil
		}
	} else {
		oldPath := sftp.JoinRemotePath(m.sftpRemoteDir, oldName)
		newPath := sftp.JoinRemotePath(m.sftpRemoteDir, newName)
		if err := m.sftpClient.Rename(m.sftpContext(), oldPath, newPath); err != nil {
			m.setStatusMsg(fmt.Sprintf("Rename failed: %v", err))
			return m, nil
		}
	}

	var err error
	m, err = m.refreshSFTPFiles()
	if err != nil {
		m.setStatusMsg(fmt.Sprintf("Refresh failed: %v", err))
	}
	return m, nil
}

func (m Model) refreshSFTPFiles() (Model, error) {
	var refreshErr error
	localFiles, err := sftp.ReadLocalDir(m.sftpLocalDir)
	if err == nil {
		m.sftpLocalFiles = localFiles
	} else {
		refreshErr = fmt.Errorf("local: %w", err)
	}
	if m.sftpClient == nil {
		if refreshErr == nil {
			refreshErr = fmt.Errorf("remote: SFTP client is not connected")
		}
		return m.normalizeSFTPScroll(), refreshErr
	}
	remoteFiles, err := m.sftpClient.ReadRemoteDir(m.sftpContext(), m.sftpRemoteDir)
	if err == nil {
		m.sftpRemoteFiles = remoteFiles
	} else if refreshErr == nil {
		refreshErr = fmt.Errorf("remote: %w", err)
	} else {
		refreshErr = fmt.Errorf("%v; remote: %w", refreshErr, err)
	}
	return m.normalizeSFTPScroll(), refreshErr
}

func readPreviewBytes(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, limit))
}

func validateRenameName(name string) error {
	if strings.TrimSpace(name) != name || name == "" {
		return fmt.Errorf("name must not be empty or padded")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("name must be a file name")
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("name must not contain path separators")
	}
	return nil
}

func isBinaryContent(s string) bool {
	for _, r := range s {
		if r == 0 {
			return true
		}
	}
	return false
}

func formatSFTPTransferError(result sftpTransferResult) string {
	if result.err == nil {
		return ""
	}
	if result.err == context.Canceled {
		return fmt.Sprintf("SFTP %s cancelled", result.direction)
	}
	msg := fmt.Sprintf("SFTP %s failed: %v", result.direction, result.err)
	if skipped := formatSkippedReport(result.report); skipped != "" {
		msg += "\n" + skipped
	}
	return fmt.Sprintf("%s\nFROM: %s\n  TO: %s", msg, result.source, result.target)
}

func formatSFTPTransferSuccess(result sftpTransferResult) string {
	status := "SFTP transfer complete"
	if result.direction == "" {
		return status
	}
	status = fmt.Sprintf("SFTP %s complete", result.direction)
	if skipped := formatSkippedReport(result.report); skipped != "" {
		status += "\n" + skipped
	}
	return fmt.Sprintf("%s\nFROM: %s\n  TO: %s", status, result.source, result.target)
}

func formatSkippedReport(report sftp.TransferReport) string {
	if !report.HasSkipped() {
		return ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Skipped %d item(s)", report.SkippedCount))
	for _, item := range report.Skipped {
		b.WriteString("\n- ")
		b.WriteString(item.Path)
		b.WriteString(": ")
		b.WriteString(item.Reason)
	}
	if remaining := report.SkippedCount - len(report.Skipped); remaining > 0 {
		b.WriteString(fmt.Sprintf("\n- ... %d more", remaining))
	}
	return b.String()
}
