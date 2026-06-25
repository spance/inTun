package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/spance/intun/internal/platform"
)

func (m Model) handleQuitConfirmKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "y", "q", "e", "ctrl+c":
		return m, tea.Quit
	case "esc", "n":
		m.confirmQuit = false
		return m, nil
	}
	return m, nil
}

func (m Model) handlePromptKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	current := m.authQueue.Current()
	if current == nil {
		m.promptMode = false
		return m, nil
	}

	switch msg.String() {
	case "enter":
		if current.Type == platform.AuthRequestHostKey {
			m.authQueue.Complete(AuthResponse{Accept: true})
		} else {
			m.authQueue.Complete(AuthResponse{Accept: true, Password: m.promptInput})
		}
		m.promptMode = false
		return m, m.pollAuthRequests()
	case "a":
		if current.Type == platform.AuthRequestHostKey {
			m.authQueue.Complete(AuthResponse{Accept: true})
			m.promptMode = false
			return m, m.pollAuthRequests()
		}
		return m, nil
	case "r", "esc":
		m.authQueue.Complete(AuthResponse{Accept: false})
		m.promptMode = false
		return m, m.pollAuthRequests()
	case "backspace":
		if len(m.promptInput) > 0 {
			m.promptInput = string([]rune(m.promptInput)[:len([]rune(m.promptInput))-1])
		}
		return m, nil
	default:
		if current.Type == platform.AuthRequestPassword {
			key := msg.Key()
			if key.Code == tea.KeySpace {
				m.promptInput += " "
			} else if key.Text != "" {
				m.promptInput += key.Text
			}
		}
		return m, nil
	}
}
