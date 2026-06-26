package tui

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
)

func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

func truncateMiddle(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	left := (max - 3) / 2
	right := max - 3 - left
	return string(runes[:left]) + "..." + string(runes[len(runes)-right:])
}

type tableLayout struct {
	nameW    int
	typeW    int
	addrW    int
	latencyW int
}

func newTableLayout(width int) tableLayout {
	width = renderWidth(width)
	layout := tableLayout{
		nameW:    10,
		typeW:    8,
		addrW:    colAddrW,
		latencyW: 7,
	}

	fixedWithoutNameAndAddr := 2 + colIDW + 1 + 3 + colStatusW + 1 + layout.typeW + 1 + 1 + 1 + layout.latencyW
	remaining := width - fixedWithoutNameAndAddr
	layout.addrW = min(colAddrW, (remaining-layout.nameW)/2)
	if layout.addrW < 10 {
		layout.addrW = 10
	}
	layout.nameW = remaining - 2*layout.addrW
	if layout.nameW < 10 {
		deficit := 10 - layout.nameW
		layout.addrW -= (deficit + 1) / 2
		if layout.addrW < 8 {
			layout.addrW = 8
		}
		layout.nameW = remaining - 2*layout.addrW
	}
	return layout
}

func renderWidth(width int) int {
	if width < minTermWidth {
		return minTermWidth
	}
	return width
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

	limit := max
	suffix := ""
	if max > 3 {
		limit = max - 3
		suffix = "..."
	}

	var b strings.Builder
	visible := 0
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			j := i + 1
			if j < len(s) && s[j] == '[' {
				j++
				for j < len(s) {
					c := s[j]
					j++
					if c >= '@' && c <= '~' {
						break
					}
				}
				b.WriteString(s[i:j])
				i = j
				continue
			}
		}

		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 0 {
			break
		}
		if visible >= limit {
			break
		}
		b.WriteRune(r)
		visible++
		i += size
	}
	if suffix != "" {
		b.WriteString("\x1b[0m")
		b.WriteString(suffix)
	}
	return b.String()
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
	if strings.Contains(addr, ":") {
		return addr
	}
	return "127.0.0.1:" + addr
}

func validPortInput(input string, allowAddr bool) bool {
	if input == "" {
		return false
	}
	if !strings.Contains(input, ":") {
		return validPort(input)
	}
	if !allowAddr {
		return false
	}
	host, port, err := net.SplitHostPort(input)
	if err != nil || host == "" {
		return false
	}
	return validPort(port)
}

func validPortInputRune(r rune, allowAddr bool) bool {
	if r >= '0' && r <= '9' {
		return true
	}
	return allowAddr && (r == '.' || r == ':')
}

func validPort(port string) bool {
	n, err := strconv.Atoi(port)
	return err == nil && n > 0 && n <= 65535
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
