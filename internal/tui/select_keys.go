package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/spance/intun/internal/platform"
	"github.com/spance/intun/internal/tunnel"
)

func (m Model) handleHostSelectKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if len(m.hosts) > 0 && m.hostCursor < len(m.hosts) {
			m.selectedHost = m.hosts[m.hostCursor]
			m.screen = ScreenSelectType
			return m, nil
		}
	case "esc", "q":
		m.screen = ScreenMain
		return m, nil
	case "up", "k":
		if m.hostCursor > 0 {
			m.hostCursor--
		}
	case "down", "j":
		if m.hostCursor < len(m.hosts)-1 {
			m.hostCursor++
		}
	case "pgup":
		m.hostCursor -= hostSelectVisibleItems(m.height)
		if m.hostCursor < 0 {
			m.hostCursor = 0
		}
	case "pgdown":
		m.hostCursor += hostSelectVisibleItems(m.height)
		if m.hostCursor >= len(m.hosts) {
			m.hostCursor = len(m.hosts) - 1
		}
	case "home":
		m.hostCursor = 0
	case "end":
		if len(m.hosts) > 0 {
			m.hostCursor = len(m.hosts) - 1
		}
	}
	m.hostScroll = clampScroll(m.hostCursor, m.hostScroll, hostSelectVisibleItems(m.height))
	return m, nil
}

func (m Model) handleTypeSelectKeys(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	items := tunnelTypeItems()
	switch keyMsg.String() {
	case "enter":
		if m.typeCursor >= 0 && m.typeCursor < len(items) {
			item := items[m.typeCursor]
			m.selectedType = item.t
			m.screen = ScreenInputPort
			m.portInput = ""
			m.inputMode = 0
			return m, nil
		}
	case "esc", "q":
		m.screen = ScreenSelectHost
		return m, nil
	case "up", "k":
		if m.typeCursor > 0 {
			m.typeCursor--
		}
	case "down", "j":
		if m.typeCursor < len(items)-1 {
			m.typeCursor++
		}
	case "pgup":
		m.typeCursor -= selectListVisibleItems(m.height, typeListHeight)
		if m.typeCursor < 0 {
			m.typeCursor = 0
		}
	case "pgdown":
		m.typeCursor += selectListVisibleItems(m.height, typeListHeight)
		if m.typeCursor >= len(items) {
			m.typeCursor = len(items) - 1
		}
	}
	m.typeScroll = clampScroll(m.typeCursor, m.typeScroll, selectListVisibleItems(m.height, typeListHeight))
	return m, nil
}

func (m Model) buildSSHConfig() *platform.SSHConfig {
	return &platform.SSHConfig{
		Host:         m.selectedHost.Hostname,
		Port:         m.selectedHost.Port,
		User:         m.selectedHost.User,
		IdentityFile: m.selectedHost.IdentityFile,
	}
}

func (m Model) handlePortInputKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if m.selectedType == tunnel.Dynamic {
			if !validPortInput(m.portInput, false) {
				m.err = fmt.Errorf("invalid SOCKS proxy port: %s", m.portInput)
				return m, nil
			}
			m.err = nil
			m.localPort = m.portInput
			m.manager.Create(m.selectedHost.Name, m.buildSSHConfig(), m.selectedType, m.localPort, "")
			m.screen = ScreenMain
			return m, nil
		}
		if m.selectedType == tunnel.Remote {
			if m.inputMode == 0 {
				if !validPortInput(m.portInput, true) {
					m.err = fmt.Errorf("invalid local target: %s", m.portInput)
					return m, nil
				}
				m.err = nil
				if strings.Contains(m.portInput, ":") {
					m.localPort = m.portInput
				} else {
					m.localPort = "127.0.0.1:" + m.portInput
				}
				m.portInput = ""
				m.inputMode = 1
				return m, nil
			}
			if !validPortInput(m.portInput, true) {
				m.err = fmt.Errorf("invalid remote listen: %s", m.portInput)
				return m, nil
			}
			m.err = nil
			if strings.Contains(m.portInput, ":") {
				m.remotePort = m.portInput
			} else {
				m.remotePort = "127.0.0.1:" + m.portInput
			}
			m.manager.Create(m.selectedHost.Name, m.buildSSHConfig(), m.selectedType, m.localPort, m.remotePort)
			m.screen = ScreenMain
			return m, nil
		}
		if m.inputMode == 0 {
			if !validPortInput(m.portInput, true) {
				m.err = fmt.Errorf("invalid local listen: %s", m.portInput)
				return m, nil
			}
			m.err = nil
			if strings.Contains(m.portInput, ":") {
				m.localPort = m.portInput
			} else {
				m.localPort = "127.0.0.1:" + m.portInput
			}
			m.portInput = ""
			m.inputMode = 1
			return m, nil
		}
		if !validPortInput(m.portInput, true) {
			m.err = fmt.Errorf("invalid remote target: %s", m.portInput)
			return m, nil
		}
		m.err = nil
		if strings.Contains(m.portInput, ":") {
			m.remotePort = m.portInput
		} else {
			m.remotePort = "127.0.0.1:" + m.portInput
		}
		m.manager.Create(m.selectedHost.Name, m.buildSSHConfig(), m.selectedType, m.localPort, m.remotePort)
		m.screen = ScreenMain
		return m, nil
	case "esc", "q":
		m.screen = ScreenSelectType
		return m, nil
	case "backspace":
		if len(m.portInput) > 0 {
			m.portInput = string([]rune(m.portInput)[:len([]rune(m.portInput))-1])
		}
	default:
		if text := msg.Key().Text; text != "" {
			allowAddr := m.selectedType == tunnel.Remote || m.selectedType == tunnel.Local
			for _, r := range text {
				if validPortInputRune(r, allowAddr) {
					m.portInput += string(r)
				}
			}
		}
	}
	return m, nil
}
