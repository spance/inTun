package tui

import (
	"os"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"
)

func checkTerminalSize() tea.Msg {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return nil
	}
	return sizeMsg{width: w, height: h}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = m.resizeComponents(msg.Width, msg.Height)
		if m.screen == ScreenSFTP {
			m = m.normalizeSFTPScroll()
		}
		return m, nil
	case sizeMsg:
		if msg.width != m.width || msg.height != m.height {
			m.width = msg.width
			m.height = msg.height
			m = m.resizeComponents(msg.width, msg.height)
			if m.screen == ScreenSFTP {
				m = m.normalizeSFTPScroll()
			}
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKeyPress(msg)
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tickMsg:
		m.sampleTunnelTraffic()
		if !m.statusConfirm && m.statusTicks > 0 {
			m.statusTicks--
			if m.statusTicks == 0 {
				m.statusMsg = ""
			}
		}
		if m.sftpTransferring && m.sftpProgress != nil {
			snapshot := m.sftpProgress.Snapshot()
			m.sftpProgress.SetSpeed((snapshot.Done - m.sftpPrevDone) * 2)
			m.sftpPrevDone = snapshot.Done
		}
		return m, tea.Batch(
			tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
				return tickMsg{}
			}),
			checkTerminalSize,
			m.pollAuthRequests(),
		)
	case authRequestMsg:
		if msg.request.Response != nil {
			m.promptMode = true
			m.promptInput = ""
		}
		return m, nil
	case sftpOpenResultMsg:
		return m.handleSFTPOpenResult(msg)
	case sftpNavigateResultMsg:
		return m.handleSFTPNavigateResult(msg), nil
	case sftpPreviewResultMsg:
		return m.handleSFTPPreviewResult(msg), nil
	case sftpRenameResultMsg:
		return m.handleSFTPRenameResult(msg)
	case sftpRefreshResultMsg:
		return m.handleSFTPRefreshResult(msg), nil
	case sftpSinglePreflightResultMsg:
		return m.handleSFTPSinglePreflightResult(msg)
	case sftpPlanResultMsg:
		return m.handleSFTPPlanResult(msg), nil
	case sftpTransferResult:
		return m.handleSFTPTransferResult(msg)
	}

	return m, nil
}
