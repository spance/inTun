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
		return m.previewSFTPFile(0, filepath.Join(m.sftpLocalDir, entry.Name))
	}
	return m.previewSFTPFile(1, sftp.JoinRemotePath(m.sftpRemoteDir, entry.Name))
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
		return m.renameSFTPEntry(0, oldPath, newPath)
	}
	oldPath := sftp.JoinRemotePath(m.sftpRemoteDir, oldName)
	newPath := sftp.JoinRemotePath(m.sftpRemoteDir, newName)
	return m.renameSFTPEntry(1, oldPath, newPath)
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
