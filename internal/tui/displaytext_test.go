package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestSafeInlineNeutralizesTerminalControls(t *testing.T) {
	input := "report\x1b]52;c;ZXZpbA==\a.txt\nnext\tname\x00"
	got := safeInline(input)

	if strings.ContainsAny(got, "\x1b\a\x00\n\t") {
		t.Fatalf("safeInline left terminal controls in %q", got)
	}
	if got != `report.txt\nnext\tname\x00` {
		t.Fatalf("safeInline() = %q", got)
	}
}

func TestSafeMultilinePreservesOnlyLineFeeds(t *testing.T) {
	got := safeMultiline("one\rignored\n二\tthree\x1b[31m!")
	if got != "one\\rignored\n二    three!" {
		t.Fatalf("safeMultiline() = %q", got)
	}
}

func TestTruncateUsesTerminalCellWidth(t *testing.T) {
	got := truncate("中文AB", 5)
	if width := ansi.StringWidthWc(got); width > 5 {
		t.Fatalf("truncate width = %d, value %q", width, got)
	}
	if strings.Contains(got, "B") {
		t.Fatalf("truncate did not shorten %q", got)
	}
}

func TestTruncateANSIPreservesWidth(t *testing.T) {
	input := "\x1b[31m中文ABCD\x1b[0m"
	got := truncateANSI(input, 6)
	if width := ansi.StringWidthWc(got); width > 6 {
		t.Fatalf("truncateANSI width = %d, value %q", width, got)
	}
}
