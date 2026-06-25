package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/spance/intun/internal/sftp"
)

func formatFileOverwriteConfirmMessage(focus int, src, dst string, overwriteReport sftp.OverwriteReport) string {
	direction, source, destination := syncDirectionLabels(focus)
	var b strings.Builder
	b.WriteString(direction)
	b.WriteString("\nOVERWRITE FILE")
	if warning := formatOverwriteWarning(overwriteReport); warning != "" {
		b.WriteString("\n")
		b.WriteString(warning)
	}
	b.WriteString(fmt.Sprintf("\nSOURCE: %s  %s\nDESTINATION: %s  %s", source, src, destination, dst))
	return b.String()
}

func formatDirectorySyncConfirmMessage(focus int, src, dst string, overwriteReport sftp.OverwriteReport) string {
	direction, source, destination := syncDirectionLabels(focus)
	var b strings.Builder
	b.WriteString(direction)
	if warning := formatOverwriteWarning(overwriteReport); warning != "" {
		b.WriteString("\n")
		b.WriteString(warning)
	}
	b.WriteString(fmt.Sprintf("\nSOURCE: %s  %s\nDESTINATION: %s  %s", source, src, destination, dst))
	return b.String()
}

func syncDirectionLabels(focus int) (direction, source, destination string) {
	direction = "LOCAL -> REMOTE  UPLOAD"
	source = "LOCAL"
	destination = "REMOTE"
	if focus == 1 {
		direction = "REMOTE -> LOCAL  DOWNLOAD"
		source = "REMOTE"
		destination = "LOCAL"
	}
	return direction, source, destination
}

func formatOverwriteWarning(report sftp.OverwriteReport) string {
	if !report.HasOverwrites() {
		return ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("OVERWRITE: %d existing target item(s)", report.Count))
	for _, item := range report.Items {
		b.WriteString("\n- ")
		b.WriteString(item.Path)
		b.WriteString(": ")
		b.WriteString(item.Kind)
	}
	if remaining := report.Count - len(report.Items); remaining > 0 {
		b.WriteString(fmt.Sprintf("\n- ... %d more", remaining))
	}
	return b.String()
}

func formatSFTPTransferError(result sftpTransferResult) string {
	if result.err == nil {
		return ""
	}
	if result.err == context.Canceled {
		return fmt.Sprintf("SFTP %s cancelled", result.direction)
	}
	msg := fmt.Sprintf("SFTP %s failed: %v", result.direction, result.err)
	if skipped := formatSkippedReport(result.report); skipped != "" {
		msg += "\n" + skipped
	}
	return fmt.Sprintf("%s\nFROM: %s\n  TO: %s", msg, result.source, result.target)
}

func formatSFTPTransferSuccess(result sftpTransferResult) string {
	status := "SFTP transfer complete"
	if result.direction == "" {
		return status
	}
	status = fmt.Sprintf("SFTP %s complete", result.direction)
	if skipped := formatSkippedReport(result.report); skipped != "" {
		status += "\n" + skipped
	}
	return fmt.Sprintf("%s\nFROM: %s\n  TO: %s", status, result.source, result.target)
}

func formatSkippedReport(report sftp.TransferReport) string {
	if !report.HasSkipped() {
		return ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Skipped %d item(s)", report.SkippedCount))
	for _, item := range report.Skipped {
		b.WriteString("\n- ")
		b.WriteString(item.Path)
		b.WriteString(": ")
		b.WriteString(item.Reason)
	}
	if remaining := report.SkippedCount - len(report.Skipped); remaining > 0 {
		b.WriteString(fmt.Sprintf("\n- ... %d more", remaining))
	}
	return b.String()
}
