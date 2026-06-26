package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spance/intun/internal/sftp"
)

type sftpEntryView struct {
	name     string
	meta     string
	modified string
	kind     string
	isDir    bool
}

func sftpEntryViewAt(files []sftp.FileEntry, index int) (sftpEntryView, bool) {
	if index == 0 {
		return sftpEntryView{name: "../", kind: "parent", isDir: true}, true
	}
	idx := index - 1
	if idx < 0 || idx >= len(files) {
		return sftpEntryView{}, false
	}
	entry := files[idx]
	name := entry.Name
	if entry.IsDir {
		name += "/"
	}
	return sftpEntryView{
		name:     name,
		meta:     sftpEntryMeta(entry),
		modified: formatSFTPModTime(entry.ModTime),
		kind:     sftp.FileEntryKind(entry),
		isDir:    entry.IsDir,
	}, true
}

func renderSFTPEntryColumns(entry sftpEntryView, width int) string {
	if width <= 0 {
		return ""
	}
	meta := entry.meta
	modified := entry.modified
	showModified := modified != "" && width >= 46
	metaWidth := 10
	modifiedWidth := 11
	gaps := 0
	if meta != "" {
		gaps++
	}
	if showModified {
		gaps++
	}
	nameWidth := width - gaps
	if meta != "" {
		nameWidth -= metaWidth
	}
	if showModified {
		nameWidth -= modifiedWidth
	}
	if nameWidth < 1 {
		nameWidth = 1
		showModified = false
		if meta != "" {
			metaWidth = min(metaWidth, max(1, width-2))
			nameWidth = width - metaWidth - 1
			if nameWidth < 1 {
				nameWidth = 1
				meta = ""
			}
		}
	}

	name := truncate(entry.name, nameWidth)
	line := fitLine(name, nameWidth)
	if meta != "" {
		line += " " + lipgloss.NewStyle().Width(metaWidth).Align(lipgloss.Right).Render(truncate(meta, metaWidth))
	}
	if showModified {
		line += " " + lipgloss.NewStyle().Width(modifiedWidth).Align(lipgloss.Right).Render(modified)
	}
	return fitLine(line, width)
}

func renderSFTPSelectedDetail(panel, dir string, files []sftp.FileEntry, cursor, width int) string {
	entry, ok := sftpEntryViewAt(files, cursor)
	if !ok {
		return fitLine(mutedStyle.Render(panel+" selected  -"), width)
	}
	if cursor == 0 {
		line := accentStyle.Render(panel) +
			mutedStyle.Render("  parent  ") +
			shortcutStyle.Render(truncateMiddle(dir, max(8, width-22))) +
			mutedStyle.Render("  Enter open")
		return fitLine(line, width)
	}

	parts := []string{entry.kind}
	if entry.meta != "" && !entry.isDir {
		parts = append(parts, entry.meta)
	}
	if entry.modified != "" {
		parts = append(parts, entry.modified)
	}
	line := accentStyle.Render(panel) +
		mutedStyle.Render("  selected  ") +
		shortcutStyle.Render(entry.name) +
		mutedStyle.Render("  "+strings.Join(parts, "  "))
	return fitLine(line, width)
}

func sftpEntryMeta(entry sftp.FileEntry) string {
	if entry.IsDir {
		return "DIR"
	}
	if entry.Mode.IsRegular() {
		return formatBytes(entry.Size)
	}
	switch sftp.FileEntryKind(entry) {
	case "symbolic link":
		return "LINK"
	case "socket":
		return "SOCK"
	case "device file":
		return "DEV"
	case "named pipe":
		return "PIPE"
	case "irregular file":
		return "IRREG"
	default:
		return "OTHER"
	}
}

func formatSFTPModTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("01-02 15:04")
}

func sftpPanelRangeLabel(fileCount, scroll, visible int) string {
	if fileCount <= 0 {
		return "0/0"
	}
	start := scroll
	if start < 1 {
		start = 1
	}
	if start > fileCount {
		start = fileCount
	}
	end := scroll + visible - 1
	if end < start {
		end = start
	}
	if end > fileCount {
		end = fileCount
	}
	return fmt.Sprintf("%d-%d/%d", start, end, fileCount)
}

func formatSFTPBreadcrumb(path string, width int) string {
	if width <= 0 {
		return ""
	}
	if path == "" {
		return "-"
	}
	if path == "/" {
		return "/"
	}
	parts := strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	if len(parts) == 0 {
		return truncateMiddle(path, width)
	}
	root := ""
	if strings.HasPrefix(path, "/") {
		root = "/ "
	}
	breadcrumb := root + strings.Join(parts, " / ")
	if lipgloss.Width(breadcrumb) <= width {
		return breadcrumb
	}
	if len(parts) <= 2 {
		return truncateMiddle(breadcrumb, width)
	}
	tail := strings.Join(parts[len(parts)-2:], " / ")
	short := root + "... / " + tail
	if lipgloss.Width(short) <= width {
		return short
	}
	return truncateMiddle(short, width)
}
