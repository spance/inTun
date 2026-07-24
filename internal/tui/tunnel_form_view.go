package tui

import (
	"fmt"
	"strings"

	"github.com/spance/intun/internal/tunnel"
)

func (m Model) renderPortInput() string {
	var b strings.Builder
	title := "Enter Port Number"
	if m.selectedProtocol == tunnel.UDP {
		title += "  UDP"
	}
	b.WriteString(headerStyle.Render(title))
	b.WriteString("\n\n")

	if m.inputMode > 0 && m.localPort != "" {
		b.WriteString(mutedStyle.Render("Local: " + m.localPort))
		b.WriteString("\n")
	}
	if step, ok := m.currentTunnelInputStep(); ok {
		b.WriteString(fmt.Sprintf("%s: %s", step.label, m.portInput))
		b.WriteString(shortcutStyle.Render("_"))
	}
	b.WriteString("\n\n")
	b.WriteString(shortcutStyle.Render("Press Enter to confirm, Esc to cancel"))
	return b.String()
}

func tunnelTypeItems() []typeItem {
	return []typeItem{
		{name: "Local TCP (-L)", desc: "Forward a local TCP port to the remote host", t: tunnel.Local, p: tunnel.TCP},
		{name: "Local UDP", desc: "Relay local UDP datagrams through the remote intun agent", t: tunnel.Local, p: tunnel.UDP},
		{name: "Remote TCP (-R)", desc: "Forward a remote TCP port to this machine", t: tunnel.Remote, p: tunnel.TCP},
		{name: "Remote UDP", desc: "Expose a remote UDP port and relay it to this machine", t: tunnel.Remote, p: tunnel.UDP},
		{name: "Dynamic TCP (-D)", desc: "SOCKS5 TCP proxy on a local port", t: tunnel.Dynamic, p: tunnel.TCP},
	}
}

func (m Model) renderTypeSelect() string {
	width := renderWidth(m.width)
	items := tunnelTypeItems()
	visibleItems := selectListVisibleItems(m.height, typeListHeight)
	m.typeScroll = clampScroll(m.typeCursor, m.typeScroll, visibleItems)
	var b strings.Builder
	b.WriteString(sectionTitleStyle.Render("Select Tunnel Type"))
	b.WriteString("\n")
	end := min(len(items), m.typeScroll+visibleItems)
	for i := m.typeScroll; i < end; i++ {
		item := items[i]
		row := item.name + "\n" + mutedStyle.Render(item.desc)
		if i == m.typeCursor {
			b.WriteString(listSelectedStyle.Width(width - 2).Render(row))
		} else {
			b.WriteString(listItemStyle.Width(width - 2).Render(row))
		}
		b.WriteString("\n")
	}
	return b.String()
}
