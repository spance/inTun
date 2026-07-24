package tui

import (
	"fmt"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/spance/intun/internal/platform"
)

func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	s = safeInline(s)
	if ansi.StringWidthWc(s) <= max {
		return s
	}
	if max <= 3 {
		return ansi.TruncateWc(s, max, "")
	}
	return ansi.TruncateWc(s, max, "...")
}

func truncateMiddle(s string, max int) string {
	if max <= 0 {
		return ""
	}
	s = safeInline(s)
	width := ansi.StringWidthWc(s)
	if width <= max {
		return s
	}
	if max <= 3 {
		return ansi.TruncateWc(s, max, "")
	}
	left := (max - 3) / 2
	right := max - 3 - left
	return ansi.CutWc(s, 0, left) + "..." + ansi.CutWc(s, width-right, width)
}

func renderWidth(width int) int {
	if width <= 0 {
		return defaultWidth
	}
	return max(1, width)
}

func renderHeight(height int) int {
	if height <= 0 {
		return defaultHeight
	}
	return height
}

func fitLine(line string, width int) string {
	if lipgloss.Width(line) > width {
		line = truncateANSI(line, width)
	}
	padding := width - lipgloss.Width(line)
	if padding > 0 {
		line += strings.Repeat(" ", padding)
	}
	return line
}

func truncateANSI(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	if max > 3 {
		return ansi.TruncateWc(s, max, "...")
	}
	return ansi.TruncateWc(s, max, "")
}

func selectListHeight(height, maxHeight int) int {
	listHeight := height - 8
	if listHeight > maxHeight {
		listHeight = maxHeight
	}
	if listHeight < 5 {
		listHeight = 5
	}
	return listHeight
}

func hostSelectVisibleItems(height int) int {
	return selectListVisibleItems(height, hostListHeight)
}

func selectListVisibleItems(height, maxItems int) int {
	rowHeight := 3
	items := selectListHeight(height, maxItems*rowHeight) / rowHeight
	if items < 1 {
		return 1
	}
	if items > maxItems {
		return maxItems
	}
	return items
}

func clampScroll(cursor, scroll, height int) int {
	if cursor < scroll {
		return cursor
	}
	if cursor >= scroll+height {
		return cursor - height + 1
	}
	if scroll < 0 {
		return 0
	}
	return scroll
}

func formatTunnelAddr(addr string) string {
	if addr == "" {
		return "-"
	}
	normalized, err := platform.ParseForwardAddress(addr, platform.ForwardAddressOptions{AllowHost: true})
	if err != nil {
		return safeInline(addr)
	}
	return normalized
}

func validPortInputRune(r rune, allowAddr bool) bool {
	if r >= '0' && r <= '9' {
		return true
	}
	return allowAddr && unicode.IsPrint(r) && !unicode.IsSpace(r)
}

func formatBytes(b int64) string {
	const KB = 1024
	const MB = KB * 1024

	if b >= MB {
		return fmt.Sprintf("%.2f MB", float64(b)/float64(MB))
	}
	return fmt.Sprintf("%.2f KB", float64(b)/float64(KB))
}

func formatTransferSpeed(bytesPerSec int64) string {
	if bytesPerSec >= 1024*1024 {
		return fmt.Sprintf("%.1fMB/s", float64(bytesPerSec)/(1024*1024))
	}
	return fmt.Sprintf("%.1fKB/s", float64(bytesPerSec)/1024)
}

func formatSpeed(bytes int64, dir string) string {
	return fmt.Sprintf("%s%s/s", dir, formatBytes(bytes))
}

func formatTotal(bytes int64, dir string) string {
	return fmt.Sprintf("%s%s", dir, formatBytes(bytes))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
