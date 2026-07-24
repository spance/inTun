package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func (m Model) View() tea.View {
	view := tea.NewView(m.renderView())
	view.AltScreen = true
	view.WindowTitle = "inTun"
	view.BackgroundColor = colorPanel
	return view
}

func (m Model) renderView() string {
	var b strings.Builder

	width := renderWidth(m.width)
	height := renderHeight(m.height)
	if width < minTermWidth || height < 8 {
		title := renderTopBar(width, "inTun", m.screenName(), m.version)
		body := lipgloss.Place(
			width,
			max(1, height-2),
			lipgloss.Center,
			lipgloss.Center,
			warningStyle.Render(fmt.Sprintf("Terminal too small\nNeed at least %d x 8", minTermWidth)),
		)
		return title + "\n" + body
	}

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
	case ScreenInputHost:
		b.WriteString(m.renderManualHostInput())
	case ScreenSelectType:
		b.WriteString(m.renderTypeSelect())
	case ScreenInputPort:
		b.WriteString(m.renderPortInput())
	case ScreenSFTP:
		b.WriteString(m.renderSFTPScreen())
	}

	content := b.String()
	shortcuts := m.renderShortcuts()
	remainingLines := height - lipgloss.Height(content+shortcuts)
	if remainingLines > 0 {
		content += strings.Repeat("\n", remainingLines)
	}
	content += shortcuts
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
	case ScreenInputHost:
		return "CONNECT"
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
	width = max(1, width)
	versionStyle := lipgloss.NewStyle().
		Background(colorSurface).
		Foreground(colorMuted).
		Padding(0, 1)
	modeStyle := lipgloss.NewStyle().
		Background(colorAccent).
		Foreground(colorPanel).
		Bold(true).
		Padding(0, 1)

	modePill := modeStyle.Render(truncate(mode, max(1, width/4)))
	versionPill := ""
	if width >= 48 {
		versionPill = versionStyle.Render(truncate(version, 16))
	}
	right := ""
	if width >= 64 {
		right = mutedStyle.Render(time.Now().Format("15:04"))
	}
	optionalWidth := lipgloss.Width(versionPill) + lipgloss.Width(modePill) + lipgloss.Width(right)
	titleBudget := width - optionalWidth
	if titleBudget < 8 && right != "" {
		optionalWidth -= lipgloss.Width(right)
		right = ""
		titleBudget = width - optionalWidth
	}
	if titleBudget < 8 && versionPill != "" {
		optionalWidth -= lipgloss.Width(versionPill)
		versionPill = ""
		titleBudget = width - optionalWidth
	}
	left := titleStyle.Render(truncate(title, max(1, titleBudget-2)))
	ruleWidth := width - lipgloss.Width(left) - optionalWidth
	rule := lipgloss.NewStyle().
		Foreground(colorMuted).
		PaddingChar('─').
		Width(max(0, ruleWidth)).
		Render("")
	result := lipgloss.JoinHorizontal(lipgloss.Center, left, versionPill, modePill, rule, right)
	return truncateANSI(result, width)
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

	rows := make([]string, 0, len(tunnels))
	selectedStart := 0
	selectedEnd := 0
	line := 0
	for i, tunnelSnapshot := range tunnels {
		row := m.renderTunnelRow(tunnelSnapshot, i, contentWidth, i == m.selectedIndex)
		rows = append(rows, row)
		rowHeight := lipgloss.Height(row)
		if i == m.selectedIndex {
			selectedStart = line
			selectedEnd = line + rowHeight - 1
		}
		line += rowHeight + 1
	}
	viewportModel := m.tunnelViewport
	viewportModel.SetWidth(contentWidth)
	viewportModel.SetHeight(max(4, m.height-9))
	viewportModel.SetContent(strings.Join(rows, "\n"))
	offset := selectedEnd - viewportModel.Height() + 1
	if offset < 0 {
		offset = 0
	}
	if selectedStart < offset {
		offset = selectedStart
	}
	viewportModel.SetYOffset(offset)
	b.WriteString(viewportModel.View())
	b.WriteString("\n")
	return b.String()
}
