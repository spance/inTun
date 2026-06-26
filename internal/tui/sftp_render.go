package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spance/intun/internal/sftp"
)

func (m Model) renderSFTPScreen() string {
	width := renderWidth(m.width)
	panelWidth := (width - 3) / 2
	if panelWidth < 20 {
		panelWidth = 20
	}

	var b strings.Builder
	panelHeight := m.sftpPanelHeight()
	localPanel := m.renderSFTPPanel("LOCAL", m.sftpLocalDir, m.sftpLocalFiles, 0, panelWidth, panelHeight, m.sftpFocus == 0)
	remotePanel := m.renderSFTPPanel("REMOTE", m.sftpRemoteDir, m.sftpRemoteFiles, 1, panelWidth, panelHeight, m.sftpFocus == 1)
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, localPanel, " ", remotePanel))
	b.WriteString("\n")
	b.WriteString(m.renderSFTPSelectedDetail(width - 2))
	b.WriteString("\n")

	if m.sftpRenaming {
		input := m.sftpRenameInput + "_"
		hint := "Rename: " + input
		confirmHint := "  [Enter]Confirm [Esc]Cancel"
		rendered := warningStyle.Render(hint) + mutedStyle.Render(confirmHint)
		b.WriteString(panelStyle(width-2, 0, true).Render(rendered))
		b.WriteString("\n")
	}

	if m.sftpTransferring {
		b.WriteString(panelStyle(width-2, 0, false).Render(m.renderSFTPProgress(width - 6)))
		b.WriteString("\n")
	}

	if m.sftpPreviewing {
		b.WriteString(panelStyle(width-2, m.sftpPreviewHeight()+2, true).Render(m.renderSFTPPreview(width-6, m.sftpPreviewHeight())))
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderSFTPPanel(label, dir string, files []sftp.FileEntry, panelIdx, width, height int, focused bool) string {
	var b strings.Builder

	innerWidth := max(1, width-4)
	visibleHeight := height - sftpPanelRowsAroundList
	if visibleHeight < sftpMinListRows {
		visibleHeight = sftpMinListRows
	}
	cursor := m.sftpCursor[panelIdx]
	scroll := m.sftpScroll[panelIdx]
	totalEntries := len(files) + 1
	rangeText := sftpPanelRangeLabel(len(files), scroll, visibleHeight)
	labelStyle := lipgloss.NewStyle().
		Foreground(colorMuted).
		Bold(true)
	countStyle := mutedStyle
	if focused {
		labelStyle = labelStyle.
			Background(colorAccent).
			Foreground(colorPanel).
			Padding(0, 1)
		countStyle = accentStyle
	}
	titleLeft := labelStyle.Render(label)
	count := countStyle.Render(fmt.Sprintf("%d items · %s", len(files), rangeText))
	title := titleLeft + lipgloss.PlaceHorizontal(max(1, innerWidth-lipgloss.Width(titleLeft)), lipgloss.Right, count)
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(mutedStyle.Render(formatSFTPBreadcrumb(dir, innerWidth)))
	b.WriteString("\n")
	b.WriteString(lineStyle.Render(strings.Repeat("─", innerWidth)))
	b.WriteString("\n")

	renderIdx := 0
	for i := scroll; i < totalEntries && renderIdx < visibleHeight; i++ {
		b.WriteString(m.renderSFTPEntryLine(files, i, cursor, width, focused))
		b.WriteString("\n")
		renderIdx++
	}
	for renderIdx < visibleHeight {
		b.WriteString(mutedStyle.Render(strings.Repeat(" ", max(1, width-4))))
		b.WriteString("\n")
		renderIdx++
	}

	return panelStyle(width, height, focused).Render(b.String())
}

func (m Model) renderSFTPEntryLine(files []sftp.FileEntry, i, cursor, width int, focused bool) string {
	entry, ok := sftpEntryViewAt(files, i)
	if !ok {
		return ""
	}

	prefix := "  "
	if i == cursor {
		prefix = "› "
	}

	targetWidth := max(1, width-4)
	leading := prefix
	leadingWidth := lipgloss.Width(leading)
	line := leading + renderSFTPEntryColumns(entry, targetWidth-leadingWidth)

	linePad := targetWidth - lipgloss.Width(line)
	if linePad > 0 {
		line += strings.Repeat(" ", linePad)
	}

	if i == cursor && focused {
		return lipgloss.NewStyle().
			Foreground(colorSelected).
			Background(colorPanelHi).
			Bold(true).
			Render(line)
	}
	if i == cursor {
		if entry.isDir {
			return lipgloss.NewStyle().Foreground(colorMuted).Render(line)
		} else {
			return inactiveStyle.Render(line)
		}
	}
	if entry.isDir {
		return dirStyle.Render(line)
	}
	return shortcutStyle.Render(line)
}

func (m Model) renderSFTPSelectedDetail(width int) string {
	if width < 1 {
		width = 1
	}
	panel := "LOCAL"
	dir := m.sftpLocalDir
	files := m.sftpLocalFiles
	cursor := m.sftpCursor[0]
	if m.sftpFocus == 1 {
		panel = "REMOTE"
		dir = m.sftpRemoteDir
		files = m.sftpRemoteFiles
		cursor = m.sftpCursor[1]
	}
	body := renderSFTPSelectedDetail(panel, dir, files, cursor, max(1, width-2))
	return lipgloss.NewStyle().
		Width(width).
		Background(colorSurface).
		Padding(0, 1).
		Render(body)
}

func (m Model) renderSFTPPreview(width, maxLines int) string {
	var b strings.Builder
	b.WriteString(accentStyle.Bold(true).Render("Preview"))
	b.WriteString("\n")

	lines := strings.Split(m.sftpPreview, "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}

	previewStyle := lipgloss.NewStyle().Foreground(colorText).Width(width)
	for _, line := range lines {
		displayLine := truncate(line, width)
		b.WriteString(previewStyle.Render(displayLine))
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) renderSFTPProgress(width int) string {
	p := m.sftpProgress
	if p == nil {
		return ""
	}
	var pct float64
	snapshot := p.Snapshot()
	if snapshot.Total > 0 {
		pct = float64(snapshot.Done) / float64(snapshot.Total)
		if pct > 1 {
			pct = 1
		}
	}

	speedStr := ""
	if snapshot.Speed > 0 {
		speedStr = formatTransferSpeed(snapshot.Speed)
	}

	barWidth := width
	if barWidth < 20 {
		barWidth = 20
	}

	pctText := fmt.Sprintf("%.0f%%", pct*100)
	rightBudget := max(4, barWidth/3)
	right := pctText
	if speedStr != "" {
		speedBudget := rightBudget - lipgloss.Width(pctText) - 2
		if speedBudget > 0 {
			right += "  " + truncate(speedStr, speedBudget)
		}
	}
	right = truncate(right, rightBudget)

	gapWidth := 2
	leftWidth := barWidth - lipgloss.Width(right) - gapWidth
	if leftWidth < 8 {
		gapWidth = 1
		leftWidth = barWidth - lipgloss.Width(right) - gapWidth
	}
	if leftWidth < 1 {
		right = truncate(right, max(1, barWidth-2))
		leftWidth = barWidth - lipgloss.Width(right) - 1
	}
	if leftWidth < 1 {
		leftWidth = 1
	}

	direction := m.sftpDirection
	fileWidth := leftWidth - lipgloss.Width(direction) - 1
	if fileWidth < 1 {
		fileWidth = 1
	}
	left := strings.TrimSpace(fmt.Sprintf("%s %s", direction, truncateMiddle(snapshot.File, fileWidth)))
	infoLine := warningStyle.Render(fitLine(left, leftWidth)) +
		mutedStyle.Render(strings.Repeat(" ", gapWidth)) +
		accentStyle.Render(right)
	infoLine = fitLine(infoLine, barWidth)

	return infoLine + "\n" + renderProgressBar(pct, barWidth)
}

func renderProgressBar(pct float64, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	if width < 1 {
		return ""
	}
	filled := int(float64(width) * pct)
	if pct > 0 && filled == 0 {
		filled = 1
	}
	if filled > width {
		filled = width
	}
	bar := successStyle.Render(strings.Repeat("━", filled)) + mutedStyle.Render(strings.Repeat("━", width-filled))
	return bar
}
