package tui

import (
	"fmt"
	"strings"

	"github.com/spance/intun/internal/platform"
	"github.com/spance/intun/internal/tunnel"
)

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
	promptLabel := "Password required"
	if current.Type == platform.AuthRequestPassphrase {
		promptLabel = "Private key passphrase required"
	}
	body := []string{fmt.Sprintf("%s  attempt %d/3", promptLabel, attempt)}
	if pending := m.authQueue.PendingCount(); pending > 0 {
		body = append(body, fmt.Sprintf("%d more auth request(s) queued", pending))
	}
	return renderModalSpec(width, ModalSpec{
		Title:    "Auth Required",
		Severity: ModalDanger,
		Body:     body,
		Fields: []ModalField{
			{Label: "Host", Value: current.Host},
			{Label: "Secret", Value: "[" + mask + "]"},
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
		status := t.Status
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
