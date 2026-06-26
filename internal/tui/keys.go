package tui

import tea "charm.land/bubbletea/v2"

func (m Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.promptMode {
		return m.handlePromptKeys(msg)
	}
	if m.confirmQuit {
		return m.handleQuitConfirmKeys(msg)
	}
	if m.statusConfirm && m.statusMsg != "" {
		return m.handleStatusConfirmKeys(msg)
	}

	switch m.screen {
	case ScreenMain:
		return m.handleMainKeys(msg)
	case ScreenSelectHost:
		return m.handleHostSelectKeys(msg)
	case ScreenSelectType:
		return m.handleTypeSelectKeys(msg)
	case ScreenInputPort:
		return m.handlePortInputKeys(msg)
	case ScreenSFTP:
		return m.handleSFTPKeys(msg)
	}
	return m, nil
}
