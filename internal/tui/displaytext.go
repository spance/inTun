package tui

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// safeInline turns untrusted text into a single printable terminal line.
func safeInline(value string) string {
	return sanitizeDisplayText(value, false)
}

// safeMultiline preserves line feeds while neutralizing terminal controls.
func safeMultiline(value string) string {
	return sanitizeDisplayText(value, true)
}

func sanitizeDisplayText(value string, multiline bool) string {
	value = ansi.Strip(value)
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		switch r {
		case '\n':
			if multiline {
				b.WriteRune(r)
			} else {
				b.WriteString(`\n`)
			}
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			if multiline {
				b.WriteString("    ")
			} else {
				b.WriteString(`\t`)
			}
		default:
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
				quoted := strconv.QuoteRune(r)
				b.WriteString(quoted[1 : len(quoted)-1])
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}
