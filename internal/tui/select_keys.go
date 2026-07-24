package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/spance/intun/internal/platform"
	"github.com/spance/intun/internal/tunnel"
)

func (m Model) handleHostSelectKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !m.hostFiltering && msg.String() == "/" {
		return m.beginHostFilter()
	}
	if !m.hostFiltering && msg.String() == "m" {
		return m.beginManualHostInput()
	}
	if m.hostFiltering && msg.String() == "esc" {
		return m.stopHostFilter(true), nil
	}

	hosts := m.filteredHosts()
	switch msg.String() {
	case "enter":
		if len(hosts) > 0 && m.hostCursor < len(hosts) {
			m.selectedHost = hosts[m.hostCursor]
			m = m.stopHostFilter(false)
			m.screen = ScreenSelectType
			return m, nil
		}
	case "esc", "q":
		if m.hostFiltering {
			var cmd tea.Cmd
			m.hostFilter, cmd = m.hostFilter.Update(msg)
			m.hostCursor = 0
			m.hostScroll = 0
			return m, cmd
		}
		m.screen = ScreenMain
		return m, nil
	case "up", "k":
		if m.hostCursor > 0 {
			m.hostCursor--
		}
	case "down", "j":
		if m.hostCursor < len(hosts)-1 {
			m.hostCursor++
		}
	case "pgup":
		m.hostCursor -= hostSelectVisibleItems(m.height)
		if m.hostCursor < 0 {
			m.hostCursor = 0
		}
	case "pgdown":
		m.hostCursor += hostSelectVisibleItems(m.height)
		if m.hostCursor >= len(hosts) {
			m.hostCursor = len(hosts) - 1
		}
	case "home":
		m.hostCursor = 0
	case "end":
		if len(hosts) > 0 {
			m.hostCursor = len(hosts) - 1
		}
	default:
		if m.hostFiltering {
			before := m.hostFilter.Value()
			var cmd tea.Cmd
			m.hostFilter, cmd = m.hostFilter.Update(msg)
			if m.hostFilter.Value() != before {
				m.hostCursor = 0
				m.hostScroll = 0
			}
			return m, cmd
		}
	}
	if m.hostCursor < 0 {
		m.hostCursor = 0
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
			m.selectedProtocol = item.p
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
	cfg := &platform.SSHConfig{
		Host:           m.selectedHost.Hostname,
		Port:           m.selectedHost.Port,
		User:           m.selectedHost.User,
		IdentityFile:   m.selectedHost.IdentityFile,
		IdentityFiles:  append([]string(nil), m.selectedHost.IdentityFiles...),
		IdentityAgent:  m.selectedHost.IdentityAgent,
		IdentitiesOnly: m.selectedHost.IdentitiesOnly,
	}
	for _, jump := range m.selectedHost.JumpHosts {
		cfg.ProxyJumps = append(cfg.ProxyJumps, platform.SSHConfig{
			Host:           jump.Hostname,
			Port:           jump.Port,
			User:           jump.User,
			IdentityFile:   jump.IdentityFile,
			IdentityFiles:  append([]string(nil), jump.IdentityFiles...),
			IdentityAgent:  jump.IdentityAgent,
			IdentitiesOnly: jump.IdentitiesOnly,
		})
	}
	return cfg
}

func (m Model) handlePortInputKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		var complete bool
		m, complete = m.acceptTunnelInput()
		if complete {
			m.completeSelectedTunnel(m.localPort, m.remotePort)
		}
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

func (m *Model) createSelectedTunnel(localAddr, remoteAddr string) {
	m.manager.CreateWithProtocol(m.selectedHost.Name, m.buildSSHConfig(), m.selectedType, m.selectedProtocol, localAddr, remoteAddr)
}
