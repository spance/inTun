package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spance/intun/internal/platform"
	"github.com/spance/intun/internal/tunnel"
)

func (m Model) View() tea.View {
	view := tea.NewView(m.renderView())
	view.AltScreen = true
	return view
}

func (m Model) renderView() string {
	var b strings.Builder

	width := renderWidth(m.width)
	height := renderHeight(m.height)

	var title string
	if m.screen == ScreenSFTP {
		title = fmt.Sprintf("inTun  %s", m.sftpHostLabel)
	} else {
		title = "inTun  Interactive SSH Tunnel"
	}
	b.WriteString(renderTopBar(width, title, m.screenName(), m.version))
	b.WriteString("\n")

	if m.screen != ScreenSFTP {
		b.WriteString("\n")
	}

	if m.err != nil {
		b.WriteString(errorStyle.Render("Error: " + truncate(m.err.Error(), width-7)))
		b.WriteString("\n\n")
	}

	switch m.screen {
	case ScreenMain:
		b.WriteString(m.renderMainScreen())
	case ScreenSelectHost:
		b.WriteString(m.renderHostSelect())
	case ScreenSelectType:
		b.WriteString(m.renderTypeSelect())
	case ScreenInputPort:
		b.WriteString(m.renderPortInput())
	case ScreenSFTP:
		b.WriteString(m.renderSFTPScreen())
	}

	content := b.String()
	lines := strings.Count(content, "\n")
	remainingLines := height - lines - 1
	if remainingLines > 0 {
		content += strings.Repeat("\n", remainingLines)
	}

	content += m.renderShortcuts()
	if overlay := m.renderStatusOverlay(width); overlay.content != "" {
		content = overlayCentered(content, overlay, width, height)
	}

	if m.promptMode {
		return overlayCentered(content, m.renderPromptModal(width), width, height)
	}
	if m.confirmQuit {
		return overlayCentered(content, m.renderQuitConfirmModal(width), width, height)
	}
	if m.pendingTunnelCreate != nil {
		return overlayCentered(content, m.renderTunnelCreateConfirmModal(width), width, height)
	}
	return content
}

func (m Model) screenName() string {
	switch m.screen {
	case ScreenMain:
		return "TUNNELS"
	case ScreenSelectHost:
		return "HOSTS"
	case ScreenSelectType:
		return "TYPES"
	case ScreenInputPort:
		return "PORT"
	case ScreenSFTP:
		return "SFTP"
	default:
		return ""
	}
}

func renderTopBar(width int, title, mode, version string) string {
	left := titleStyle.Render(" " + title + " ")
	versionPill := lipgloss.NewStyle().
		Background(colorSurface).
		Foreground(colorMuted).
		Padding(0, 1).
		Render(version)
	modePill := lipgloss.NewStyle().
		Background(colorAccent).
		Foreground(colorPanel).
		Bold(true).
		Padding(0, 1).
		Render(mode)
	right := mutedStyle.Render(time.Now().Format("15:04"))
	centerWidth := width - lipgloss.Width(left) - lipgloss.Width(versionPill) - lipgloss.Width(modePill) - lipgloss.Width(right)
	if centerWidth < 1 {
		centerWidth = 1
	}
	rule := lipgloss.NewStyle().
		Foreground(colorMuted).
		PaddingChar('─').
		Width(centerWidth).
		Render("")
	return lipgloss.JoinHorizontal(lipgloss.Center, left, versionPill, modePill, rule, right)
}

func (m Model) renderMainScreen() string {
	var b strings.Builder
	tunnels := m.manager.List()

	if len(tunnels) == 0 {
		empty := panelStyle(renderWidth(m.width), 5, false).
			Render(lipgloss.Place(0, 3, lipgloss.Center, lipgloss.Center, mutedStyle.Render("No tunnels active. Press 'c' to create one.")))
		return empty + "\n"
	}

	lineWidth := renderWidth(m.width)
	contentWidth := lineWidth - 2
	if contentWidth < 60 {
		contentWidth = lineWidth
	}

	b.WriteString(m.renderTunnelSummary(contentWidth, len(tunnels)))
	b.WriteString("\n")

	for i, t := range tunnels {
		b.WriteString(m.renderTunnelRow(t, i, contentWidth, i == m.selectedIndex))
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) renderPortInput() string {
	var b strings.Builder
	title := "Enter Port Number"
	if m.selectedProtocol == tunnel.UDP {
		title += "  UDP"
	}
	b.WriteString(headerStyle.Render(title))
	b.WriteString("\n\n")

	if m.selectedType == tunnel.Dynamic {
		b.WriteString(fmt.Sprintf("SOCKS Proxy Port: %s", m.portInput))
		b.WriteString(shortcutStyle.Render("_"))
	} else if m.selectedType == tunnel.Remote {
		if m.inputMode == 0 {
			b.WriteString(fmt.Sprintf("Local Target (ip:port or port): %s", m.portInput))
			b.WriteString(shortcutStyle.Render("_"))
		} else {
			b.WriteString(fmt.Sprintf("Local Target: %s\n", m.localPort))
			b.WriteString(fmt.Sprintf("Remote Listen (ip:port or port): %s", m.portInput))
			b.WriteString(shortcutStyle.Render("_"))
		}
	} else {
		if m.inputMode == 0 {
			b.WriteString(fmt.Sprintf("Local Listen (ip:port or port): %s", m.portInput))
			b.WriteString(shortcutStyle.Render("_"))
		} else {
			b.WriteString(fmt.Sprintf("Local Listen: %s\n", m.localPort))
			b.WriteString(fmt.Sprintf("Remote Target (ip:port or port): %s", m.portInput))
			b.WriteString(shortcutStyle.Render("_"))
		}
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

func (m Model) renderHostSelect() string {
	width := renderWidth(m.width)
	visibleItems := hostSelectVisibleItems(m.height)
	m.hostScroll = clampScroll(m.hostCursor, m.hostScroll, visibleItems)

	var b strings.Builder
	title := "Select Host"
	if len(m.hosts) > 0 {
		end := min(len(m.hosts), m.hostScroll+visibleItems)
		title = fmt.Sprintf("Select Host  %d-%d/%d", m.hostScroll+1, end, len(m.hosts))
	}
	b.WriteString(sectionTitleStyle.Render(title))
	b.WriteString("\n")
	if len(m.hosts) == 0 {
		b.WriteString(panelStyle(width-2, 5, false).Render(mutedStyle.Render("No hosts found in ~/.ssh/config")))
		b.WriteString("\n")
		return b.String()
	}
	end := min(len(m.hosts), m.hostScroll+visibleItems)
	for i := m.hostScroll; i < end; i++ {
		h := m.hosts[i]
		name := h.Hostname
		if name == "" {
			name = h.Name
		}
		title := name
		if len(h.Labels) > 0 {
			title += "  "
		}
		desc := fmt.Sprintf("%s@%s:%s", h.User, h.Hostname, h.Port)
		renderedTitle := selectedStyle.Render(truncate(title, width-4))
		for _, label := range h.Labels {
			renderedTitle += labelPillStyle.Render(label)
		}
		row := renderedTitle + "\n" + mutedStyle.Render(truncate(desc, width-4))
		if i == m.hostCursor {
			b.WriteString(listSelectedStyle.Width(width - 2).Render(row))
		} else {
			b.WriteString(listItemStyle.Width(width - 2).Render(row))
		}
		b.WriteString("\n")
	}
	return b.String()
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

func (m Model) renderPromptModal(width int) ModalView {
	current := m.authQueue.Current()
	if current == nil {
		return ModalView{}
	}

	if current.Type == platform.AuthRequestHostKey {
		body := []string{"Unknown host key"}
		if pending := m.authQueue.PendingCount(); pending > 0 {
			body = append(body, fmt.Sprintf("%d more auth request(s) queued", pending))
		}
		return renderModalSpec(width, ModalSpec{
			Title:    "Auth Required",
			Severity: ModalDanger,
			Body:     body,
			Fields: []ModalField{
				{Label: "Host", Value: current.Host},
				{Label: "Fingerprint", Value: current.Fingerprint},
			},
			Actions: []ModalAction{
				{Key: "A", Label: "Accept"},
				{Key: "R", Label: "Reject"},
			},
			Width: min(64, width-8),
		})
	}

	attempt := current.RetryCount + 1
	mask := strings.Repeat("*", len([]rune(m.promptInput)))
	body := []string{fmt.Sprintf("Password required  attempt %d/3", attempt)}
	if pending := m.authQueue.PendingCount(); pending > 0 {
		body = append(body, fmt.Sprintf("%d more auth request(s) queued", pending))
	}
	return renderModalSpec(width, ModalSpec{
		Title:    "Auth Required",
		Severity: ModalDanger,
		Body:     body,
		Fields: []ModalField{
			{Label: "Host", Value: current.Host},
			{Label: "Password", Value: "[" + mask + "]"},
		},
		Actions: []ModalAction{
			{Key: "Enter", Label: "Submit"},
			{Key: "Esc", Label: "Cancel"},
		},
		Width: min(64, width-8),
	})
}

func (m Model) renderQuitConfirmModal(width int) ModalView {
	liveCount := 0
	for _, t := range m.manager.List() {
		status := t.GetStatus()
		if status == tunnel.StatusRunning || status == tunnel.StatusConnecting {
			liveCount++
		}
	}

	return renderModalSpec(width, ModalSpec{
		Title:    "Confirm Exit",
		Severity: ModalWarning,
		Body: []string{
			"Active tunnels are still running",
			fmt.Sprintf("%d live tunnel(s) will be closed when inTun exits.", liveCount),
		},
		Actions: []ModalAction{
			{Key: "Enter/Y/Q", Label: "Exit"},
			{Key: "Esc/N", Label: "Cancel"},
		},
		Width: 56,
	})
}

func (m Model) renderStatusOverlay(width int) ModalView {
	if m.sftpOverwriteConfirm {
		body, fields := modalMessageParts(m.sftpOverwriteConfirmMsg)
		return renderModalSpec(width, ModalSpec{
			Title:    "Confirm Overwrite",
			Severity: ModalDanger,
			Body:     body,
			Fields:   fields,
			Actions: []ModalAction{
				{Key: "Enter", Label: "Overwrite"},
				{Key: "Esc", Label: "Cancel"},
			},
		})
	}
	if m.sftpSyncConfirm {
		body, fields := modalMessageParts(m.sftpSyncConfirmMsg)
		return renderModalSpec(width, ModalSpec{
			Title:    "Confirm Directory Sync",
			Severity: ModalWarning,
			Body:     body,
			Fields:   fields,
			Actions: []ModalAction{
				{Key: "Enter", Label: "Confirm"},
				{Key: "Esc", Label: "Cancel"},
			},
		})
	}
	if m.statusMsg == "" {
		return ModalView{}
	}
	body, fields := modalMessageParts(m.statusMsg)
	var actions []ModalAction
	if m.statusConfirm {
		actions = []ModalAction{{Key: "Enter/Esc", Label: "OK"}}
	}
	return renderModalSpec(width, ModalSpec{
		Title:   "Notice",
		Body:    body,
		Fields:  fields,
		Actions: actions,
	})
}
