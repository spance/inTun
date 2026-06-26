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

	count := fmt.Sprintf("%d items", len(files))
	labelStyle := lipgloss.NewStyle().
		Foreground(colorMuted).
		Bold(true).
		Padding(0, 1)
	if focused {
		labelStyle = labelStyle.
			Background(colorAccent).
			Foreground(colorPanel)
	}
	title := labelStyle.Render(label) + " " + mutedStyle.Render(count)
	dirWidth := max(10, width-lipgloss.Width(title)-4)
	dirText := mutedStyle.Render(truncateMiddle(dir, min(dirWidth, max(10, width/2))))
	title = title + lipgloss.PlaceHorizontal(max(1, width-lipgloss.Width(title)-2), lipgloss.Right, dirText)
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(lineStyle.Render(strings.Repeat("─", max(1, width-4))))
	b.WriteString("\n")

	visibleHeight := height - sftpPanelRowsAroundList
	if visibleHeight < sftpMinListRows {
		visibleHeight = sftpMinListRows
	}

	cursor := m.sftpCursor[panelIdx]
	scroll := m.sftpScroll[panelIdx]

	totalEntries := len(files) + 1
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
	var name, sizeStr string
	var isDir bool

	if i == 0 {
		name = ".."
		isDir = true
	} else {
		idx := i - 1
		if idx >= len(files) {
			return ""
		}
		entry := files[idx]
		name = entry.Name
		isDir = entry.IsDir
		if !isDir && entry.Size > 0 {
			sizeStr = formatBytes(entry.Size)
		}
	}

	displayName := name
	if isDir && i > 0 {
		displayName = name + "/"
	}

	prefix := "  "
	if i == cursor {
		prefix = "❯ "
	}
	marker := " "
	if isDir {
		marker = "/"
	}

	targetWidth := max(1, width-4)
	leading := prefix + marker + " "
	leadingWidth := lipgloss.Width(leading)

	var line string
	if sizeStr != "" {
		sizeWidth := lipgloss.Width(sizeStr)
		if sizeWidth > targetWidth-leadingWidth-2 {
			sizeStr = truncate(sizeStr, max(1, targetWidth-leadingWidth-2))
			sizeWidth = lipgloss.Width(sizeStr)
		}
		nameWidth := targetWidth - leadingWidth - sizeWidth - 1
		if nameWidth < 1 {
			nameWidth = 1
		}
		truncated := truncate(displayName, nameWidth)
		padLen := targetWidth - leadingWidth - lipgloss.Width(truncated) - sizeWidth
		if padLen < 0 {
			padLen = 0
		}
		line = leading + truncated + strings.Repeat(" ", padLen) + sizeStr
	} else {
		line = leading + truncate(displayName, targetWidth-leadingWidth)
	}

	linePad := targetWidth - lipgloss.Width(line)
	if linePad > 0 {
		line += strings.Repeat(" ", linePad)
	}

	if i == cursor && focused {
		return lipgloss.NewStyle().Foreground(colorSelected).Background(colorPanelHi).Bold(true).Render(line)
	}
	if i == cursor {
		if isDir {
			return dirStyle.Render(line)
		} else {
			return inactiveStyle.Render(line)
		}
	}
	if isDir {
		return dirStyle.Render(line)
	}
	return shortcutStyle.Render(line)
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

	var speedStr string
	if snapshot.Speed > 0 {
		speedStr = " " + formatTransferSpeed(snapshot.Speed)
	}

	barWidth := width
	if barWidth < 20 {
		barWidth = 20
	}

	infoLine := warningStyle.Render(fmt.Sprintf("%s %s %.0f%%%s", m.sftpDirection, snapshot.File, pct*100, speedStr))

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
