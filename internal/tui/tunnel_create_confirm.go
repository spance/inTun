package tui

import (
	"net"

	tea "charm.land/bubbletea/v2"
	"github.com/spance/intun/internal/tunnel"
)

func (m *Model) completeSelectedTunnel(localAddr, remoteAddr string) {
	if m.selectedType == tunnel.Remote && m.selectedProtocol == tunnel.UDP && remoteUDPBindRequiresConfirmation(remoteAddr) {
		m.pendingTunnelCreate = &pendingTunnelCreate{localAddr: localAddr, remoteAddr: remoteAddr}
		return
	}
	m.createSelectedTunnel(localAddr, remoteAddr)
	m.screen = ScreenMain
}

func remoteUDPBindRequiresConfirmation(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return true
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsLoopback()
}

func (m Model) handleTunnelCreateConfirmKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "y":
		pending := m.pendingTunnelCreate
		m.pendingTunnelCreate = nil
		if pending != nil {
			m.createSelectedTunnel(pending.localAddr, pending.remoteAddr)
			m.screen = ScreenMain
		}
	case "esc", "n":
		m.pendingTunnelCreate = nil
	}
	return m, nil
}

func (m Model) renderTunnelCreateConfirmModal(width int) ModalView {
	pending := m.pendingTunnelCreate
	if pending == nil {
		return ModalView{}
	}
	return renderModalSpec(width, ModalSpec{
		Title:    "Expose Remote UDP Port?",
		Severity: ModalWarning,
		Body: []string{
			"The remote intun process will bind a non-loopback UDP socket.",
			"Confirm that remote firewall and network exposure are intentional.",
		},
		Fields: []ModalField{
			{Label: "Direction", Value: "REMOTE UDP -> LOCAL UDP"},
			{Label: "Remote listen", Value: pending.remoteAddr},
			{Label: "Local target", Value: pending.localAddr},
		},
		Actions: []ModalAction{
			{Key: "Enter/y", Label: "Expose"},
			{Key: "Esc/n", Label: "Edit"},
		},
	})
}
