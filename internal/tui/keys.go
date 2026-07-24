package tui

import tea "charm.land/bubbletea/v2"

func (m Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" && !m.confirmQuit {
		if m.hasLiveTunnels() {
			m.confirmQuit = true
			return m, nil
		}
		return m, tea.Quit
	}
	if m.promptMode {
		return m.handlePromptKeys(msg)
	}
	if m.confirmQuit {
		return m.handleQuitConfirmKeys(msg)
	}
	if m.pendingTunnelCreate != nil {
		return m.handleTunnelCreateConfirmKeys(msg)
	}
	if m.statusConfirm && m.statusMsg != "" {
		return m.handleStatusConfirmKeys(msg)
	}

	switch m.screen {
	case ScreenMain:
		return m.handleMainKeys(msg)
	case ScreenSelectHost:
		return m.handleHostSelectKeys(msg)
	case ScreenInputHost:
		return m.handleManualHostKeys(msg)
	case ScreenSelectType:
		return m.handleTypeSelectKeys(msg)
	case ScreenInputPort:
		return m.handlePortInputKeys(msg)
	case ScreenSFTP:
		return m.handleSFTPKeys(msg)
	}
	return m, nil
}
