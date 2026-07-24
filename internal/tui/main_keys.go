package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/spance/intun/internal/platform"
	"github.com/spance/intun/internal/tunnel"
)

func (m Model) handleMainKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	tunnels := m.manager.List()

	switch msg.String() {
	case "c":
		if len(m.hosts) == 0 {
			return m.beginManualHostInput()
		}
		m.err = nil
		m.screen = ScreenSelectHost
		return m, nil
	case "y":
		if len(tunnels) > 0 && m.selectedIndex < len(tunnels) {
			t := tunnels[m.selectedIndex]
			if t.Status == tunnel.StatusError && t.Failure != nil && t.Failure.Code == platform.FailureHostKeyNotCached {
				m.manager.Restart(t.ID)
			}
		}
	case "r":
		if len(tunnels) > 0 && m.selectedIndex < len(tunnels) {
			m.manager.Restart(tunnels[m.selectedIndex].ID)
		} else {
			m.setStatusMsg("No tunnel selected")
		}
	case "s":
		if len(tunnels) > 0 && m.selectedIndex < len(tunnels) {
			t := tunnels[m.selectedIndex]
			if t.Status == tunnel.StatusRunning {
				m.manager.Stop(t.ID)
			} else if t.Status == tunnel.StatusStopped {
				m.manager.Restart(t.ID)
			} else if t.Status == tunnel.StatusConnecting {
				m.manager.Stop(t.ID)
			} else if t.Status == tunnel.StatusError {
				m.setStatusMsg("Use [r] to reconnect")
			}
		} else {
			m.setStatusMsg("No tunnel selected")
		}
	case "d":
		if len(tunnels) > 0 && m.selectedIndex < len(tunnels) {
			m.manager.Delete(tunnels[m.selectedIndex].ID)
			if m.selectedIndex > 0 {
				m.selectedIndex--
			}
		} else {
			m.setStatusMsg("No tunnel selected")
		}
	case "f":
		if len(tunnels) > 0 && m.selectedIndex < len(tunnels) {
			t := tunnels[m.selectedIndex]
			if t.Status == tunnel.StatusRunning {
				return m.openSFTP(t)
			} else {
				m.setStatusMsg("Tunnel must be running to use SFTP")
			}
		} else {
			m.setStatusMsg("No tunnel selected")
		}
	case "up", "k":
		if m.selectedIndex > 0 {
			m.selectedIndex--
		}
	case "down", "j":
		if m.selectedIndex < len(tunnels)-1 {
			m.selectedIndex++
		}
	case "e", "q", "ctrl+c":
		if m.hasLiveTunnels() {
			m.confirmQuit = true
			return m, nil
		}
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) hasLiveTunnels() bool {
	for _, t := range m.manager.List() {
		status := t.Status
		if status == tunnel.StatusRunning || status == tunnel.StatusConnecting {
			return true
		}
	}
	return false
}
