package tui

import (
	"fmt"
	"os"
	"time"

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
		if m.screen == ScreenSFTP {
			m = m.normalizeSFTPScroll()
		}
		return m, nil
	case sizeMsg:
		if msg.width != m.width || msg.height != m.height {
			m.width = msg.width
			m.height = msg.height
			if m.screen == ScreenSFTP {
				m = m.normalizeSFTPScroll()
			}
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKeyPress(msg)
	case tickMsg:
		m.sampleTunnelTraffic()
		if m.statusTicks > 0 {
			m.statusTicks--
			if m.statusTicks == 0 {
				m.statusMsg = ""
			}
		}
		if m.sftpDone != nil {
			if m.sftpTransferring && m.sftpProgress != nil {
				snapshot := m.sftpProgress.Snapshot()
				m.sftpProgress.SetSpeed((snapshot.Done - m.sftpPrevDone) * 2)
				m.sftpPrevDone = snapshot.Done
			}
			select {
			case result := <-m.sftpDone:
				if result.id != m.sftpTransferID {
					m.sftpDone = nil
				} else {
					m.sftpTransferring = false
					m.sftpCancel = nil
					if m.sftpProgress != nil {
						m.sftpProgress.SetActive(false)
					}
					if m.screen == ScreenSFTP {
						if result.err != nil {
							m.setStatusMsg(formatSFTPTransferError(result))
						} else {
							m.setStatusMsg(formatSFTPTransferSuccess(result))
						}
					}
					if result.err == nil && m.screen == ScreenSFTP && m.sftpClient != nil {
						var refreshErr error
						m, refreshErr = m.refreshSFTPFiles()
						if refreshErr != nil {
							m.setStatusMsg(fmt.Sprintf("Transfer finished, but refresh failed: %v", refreshErr))
						}
					}
					m.sftpDone = nil
				}
			default:
			}
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
	}

	return m, nil
}
