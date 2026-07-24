package tui

import (
	"context"
	"errors"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	sftplib "github.com/pkg/sftp"
	"github.com/spance/intun/internal/sftp"
	"github.com/spance/intun/internal/tunnel"
)

type sftpOpenResultMsg struct {
	operationID int
	client      *sftp.Client
	localDir    string
	remoteDir   string
	localFiles  []sftp.FileEntry
	remoteFiles []sftp.FileEntry
	tunnelID    int
	hostLabel   string
	err         error
}

type sftpNavigateResultMsg struct {
	operationID int
	focus       int
	path        string
	files       []sftp.FileEntry
	err         error
}

func (m Model) beginSFTPOperation(label string) (Model, int, context.Context) {
	if m.sftpOperationCancel != nil {
		m.sftpOperationCancel()
	}
	m.sftpOperationID++
	ctx, cancel := context.WithCancel(m.sftpContext())
	m.sftpOperationCancel = cancel
	m.sftpLoading = true
	m.sftpLoadingLabel = label
	return m, m.sftpOperationID, ctx
}

func (m Model) finishSFTPOperation(operationID int) (Model, bool) {
	if operationID != m.sftpOperationID {
		return m, false
	}
	if m.sftpOperationCancel != nil {
		m.sftpOperationCancel()
	}
	m.sftpOperationCancel = nil
	m.sftpLoading = false
	m.sftpLoadingLabel = ""
	return m, true
}

func (m Model) openSFTP(snapshot tunnel.Snapshot) (Model, tea.Cmd) {
	if m.sftpLoading {
		m.setStatusMsg("SFTP is already opening")
		return m, nil
	}
	m, operationID, operationCtx := m.beginSFTPOperation("Opening SFTP")
	m.setStatusMsg("Opening SFTP...")

	manager := m.manager
	return m, func() tea.Msg {
		result := sftpOpenResultMsg{
			operationID: operationID,
			tunnelID:    snapshot.ID,
			hostLabel:   fmt.Sprintf("%s@%s:%s", snapshot.SSHConfig.User, snapshot.SSHConfig.Host, snapshot.SSHConfig.Port),
		}
		rawClient, err := manager.GetSFTPClient(snapshot.ID)
		if err != nil {
			result.err = err
			return result
		}
		sftpRaw, ok := rawClient.(*sftplib.Client)
		if !ok {
			result.err = fmt.Errorf("invalid SFTP client type")
			return result
		}
		client := sftp.NewClient(sftpRaw)
		result.client = client
		result.localDir, err = os.Getwd()
		if err != nil {
			result.err = fmt.Errorf("local working directory: %w", err)
			_ = client.Close()
			result.client = nil
			return result
		}
		result.remoteDir, err = sftpRaw.Getwd()
		if err != nil {
			result.err = fmt.Errorf("remote working directory: %w", err)
			_ = client.Close()
			result.client = nil
			return result
		}
		if result.remoteDir == "" {
			result.remoteDir = "/"
		}
		result.localFiles, err = sftp.ReadLocalDir(result.localDir)
		if err != nil {
			result.err = fmt.Errorf("read local directory: %w", err)
			_ = client.Close()
			result.client = nil
			return result
		}
		result.remoteFiles, err = client.ReadRemoteDir(operationCtx, result.remoteDir)
		if err != nil {
			result.err = fmt.Errorf("read remote directory: %w", err)
			_ = client.Close()
			result.client = nil
		}
		return result
	}
}

func (m Model) handleSFTPOpenResult(msg sftpOpenResultMsg) (Model, tea.Cmd) {
	var current bool
	m, current = m.finishSFTPOperation(msg.operationID)
	if !current {
		if msg.client == nil {
			return m, nil
		}
		return m, func() tea.Msg {
			_ = msg.client.Close()
			return nil
		}
	}
	if msg.err != nil {
		m.err = msg.err
		return m, nil
	}
	m.err = nil
	m.statusMsg = ""
	m.sftpClient = msg.client
	m.sftpLocalDir = msg.localDir
	m.sftpRemoteDir = msg.remoteDir
	m.sftpLocalFiles = msg.localFiles
	m.sftpRemoteFiles = msg.remoteFiles
	m.sftpFocus = 0
	m.sftpCursor = [2]int{}
	m.sftpScroll = [2]int{}
	m.sftpTransferring = false
	m.sftpPreview = ""
	m.sftpPreviewing = false
	m.sftpCancel = nil
	m.sftpTunnelID = msg.tunnelID
	m.sftpHostLabel = msg.hostLabel
	m.sftpDirection = ""
	m.screen = ScreenSFTP
	return m.normalizeSFTPScroll(), nil
}

func (m Model) navigateSFTP(focus int, path string) (Model, tea.Cmd) {
	m, operationID, ctx := m.beginSFTPOperation("Opening directory")
	client := m.sftpClient
	return m, func() tea.Msg {
		result := sftpNavigateResultMsg{operationID: operationID, focus: focus, path: path}
		if err := ctx.Err(); err != nil {
			result.err = err
			return result
		}
		if focus == 0 {
			result.files, result.err = sftp.ReadLocalDir(path)
		} else if client == nil {
			result.err = fmt.Errorf("SFTP client is not connected")
		} else {
			result.files, result.err = client.ReadRemoteDir(ctx, path)
		}
		return result
	}
}

func (m Model) handleSFTPNavigateResult(msg sftpNavigateResultMsg) Model {
	var current bool
	m, current = m.finishSFTPOperation(msg.operationID)
	if !current {
		return m
	}
	if msg.err != nil {
		if errors.Is(msg.err, context.Canceled) {
			m.setStatusMsg("Operation cancelled")
		} else {
			m.setStatusMsg(fmt.Sprintf("Cannot open directory: %v", msg.err))
		}
		return m
	}
	if msg.focus == 0 {
		m.sftpLocalDir = msg.path
		m.sftpLocalFiles = msg.files
	} else {
		m.sftpRemoteDir = msg.path
		m.sftpRemoteFiles = msg.files
	}
	m.sftpCursor[msg.focus] = 0
	m.sftpScroll[msg.focus] = 0
	return m.normalizeSFTPScroll()
}
