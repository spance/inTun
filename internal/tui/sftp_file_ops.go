package tui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/spance/intun/internal/sftp"
)

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
