package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spance/intun/internal/tunnel"
)

func (m Model) renderTunnelSummary(width int, total int) string {
	var running, connecting, stopped, errored int
	var tx, rx int64
	for _, t := range m.manager.List() {
		switch t.GetStatus() {
		case tunnel.StatusRunning:
			running++
		case tunnel.StatusConnecting:
			connecting++
		case tunnel.StatusStopped:
			stopped++
		case tunnel.StatusError:
			errored++
		}
		tx += t.UploadBytes
		rx += t.DownloadBytes
	}
	cards := []string{
		metricPill("ALL", fmt.Sprintf("%d", total), accentStyle, false),
		metricPill("RUN", fmt.Sprintf("%d", running), successStyle, running > 0),
		metricPill("CONN", fmt.Sprintf("%d", connecting), warningStyle, connecting > 0),
		metricPill("ERR", fmt.Sprintf("%d", errored), dangerStyle, errored > 0),
		metricPill("STOP", fmt.Sprintf("%d", stopped), mutedStyle, false),
	}
	left := lipgloss.JoinHorizontal(lipgloss.Center, cards...)
	right := metricPill("FLOW", formatTotal(tx, "TX")+"  "+formatTotal(rx, "RX"), accentStyle, false)
	innerWidth := width - 2
	if innerWidth < 1 {
		innerWidth = width
	}
	gapWidth := innerWidth - lipgloss.Width(left) - lipgloss.Width(right)
	if gapWidth < 1 {
		gapWidth = 1
	}
	body := left + strings.Repeat(" ", gapWidth) + right
	if lipgloss.Width(body) > innerWidth {
		body = truncateANSI(body, innerWidth)
	}
	return lipgloss.NewStyle().
		Width(width).
		Border(uiBorder(), false, false, true, false).
		BorderForeground(colorMuted).
		PaddingBottom(1).
		Render(body)
}

func metricPill(label, value string, valueStyle lipgloss.Style, emphasized bool) string {
	style := lipgloss.NewStyle().
		Background(colorPanel).
		Padding(0, 1).
		MarginRight(1)
	if emphasized {
		style = style.Background(colorGlass)
	}
	return style.Render(eyebrowStyle.Render(label) + " " + valueStyle.Bold(true).Render(value))
}

func (m Model) renderTunnelRow(t *tunnel.Tunnel, idx, width int, focused bool) string {
	status := tunnelStatusLabel(t)
	borderColor := colorSurface
	if status == "Error" {
		borderColor = colorDangerDim
	}
	if focused {
		borderColor = colorAccent
		if status == "Error" {
			borderColor = colorDanger
		}
	}
	rowTextStyle := lipgloss.NewStyle()
	rowMutedStyle := mutedStyle
	rowAccentStyle := accentStyle
	rowSelectedStyle := selectedStyle
	local := formatTunnelAddr(t.LocalPort)
	remote := "-"
	if t.Type == tunnel.Dynamic {
		remote = "SOCKS5"
	} else if t.RemotePort != "" {
		remote = formatTunnelAddr(t.RemotePort)
	}
	latency := "-"
	if t.Latency > 0 {
		latency = fmt.Sprintf("%dms", t.Latency.Milliseconds())
	}
	statusBadge := lipgloss.NewStyle().
		Foreground(colorPanel).
		Background(statusColor(status)).
		Bold(true).
		Padding(0, 1).
		Render(status)
	textW := width - 4
	nameMax := textW - lipgloss.Width(statusBadge) - 18
	if nameMax < 14 {
		nameMax = 14
	}
	left := rowMutedStyle.Render(fmt.Sprintf("#%d", t.ID)) + rowTextStyle.Render(" ") + rowSelectedStyle.Render(truncate(t.Name, nameMax))
	right := statusBadge
	first := left + lipgloss.PlaceHorizontal(textW-lipgloss.Width(left), lipgloss.Right, right)
	uploadSpeed := formatSpeed(t.UploadSpeed, "↑")
	downloadSpeed := formatSpeed(t.DownloadSpeed, "↓")
	uploadTotal := formatTotal(t.UploadBytes, "TX")
	downloadTotal := formatTotal(t.DownloadBytes, "RX")
	metrics := fmt.Sprintf("%s %s  %s %s", uploadSpeed, downloadSpeed, uploadTotal, downloadTotal)
	secondRight := fmt.Sprintf("%s   %s", latency, metrics)
	rightWidth := lipgloss.Width(secondRight)
	maxRightWidth := max(12, textW-18)
	if rightWidth > maxRightWidth {
		secondRight = truncate(secondRight, maxRightWidth)
		rightWidth = lipgloss.Width(secondRight)
	}
	leftWidth := textW - rightWidth - 1
	if leftWidth < 12 {
		leftWidth = 12
	}
	route := fmt.Sprintf("%s → %s", local, remote)
	typeLabel := tunnelTypeLabel(t.Type)
	secondLeft := rowAccentStyle.Render(typeLabel) + rowTextStyle.Render("  ") + rowTextStyle.Render(truncate(route, leftWidth-lipgloss.Width(typeLabel)-2))
	rightRendered := rowMutedStyle.Width(rightWidth).Render(secondRight)
	second := fitLine(secondLeft, leftWidth) + " " + rightRendered
	flow := renderTunnelFlowLine(t, m.trafficHist[t.ID], textW)
	lines := []string{first, second, flow}
	if t.Status == tunnel.StatusError && t.Error != "" {
		lines = append(lines, renderTunnelErrorHint(t, textW-2)...)
	}
	rowStyle := lipgloss.NewStyle().
		Width(width).
		Border(uiBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		MarginBottom(1)
	if focused {
		rowStyle = rowStyle.BorderForeground(borderColor)
	}
	_ = idx
	return rowStyle.Render(strings.Join(lines, "\n"))
}

func renderTunnelFlowLine(t *tunnel.Tunnel, history []int64, width int) string {
	switch t.GetStatus() {
	case tunnel.StatusRunning, tunnel.StatusConnecting:
		return renderTrafficFlow(history, width)
	case tunnel.StatusError:
		return dangerStyle.Render(fitLine("flow paused  reconnect available", width))
	case tunnel.StatusStopped:
		return mutedStyle.Render(fitLine("flow stopped", width))
	default:
		return mutedStyle.Render(fitLine("flow unavailable", width))
	}
}

func renderTrafficFlow(history []int64, width int) string {
	if width < 20 {
		width = 20
	}
	graphWidth := width
	if graphWidth < 8 {
		graphWidth = 8
	}
	if len(history) == 0 {
		return mutedStyle.Render(strings.Repeat("·", graphWidth))
	}
	if len(history) > graphWidth {
		history = history[len(history)-graphWidth:]
	}
	pad := graphWidth - len(history)
	scale := int64(16 * 1024)
	for _, sample := range history {
		if sample > scale {
			scale = sample
		}
	}
	scale = max(scale, 64*1024)

	blocks := []rune("▁▂▃▄▅▆▇█")
	var b strings.Builder
	if pad > 0 {
		b.WriteString(mutedStyle.Render(strings.Repeat("·", pad)))
	}
	for _, sample := range history {
		if sample <= 0 {
			b.WriteString(mutedStyle.Render("·"))
			continue
		}
		level := int((sample * int64(len(blocks)-1)) / scale)
		if level < 0 {
			level = 0
		}
		if level >= len(blocks) {
			level = len(blocks) - 1
		}
		style := mutedStyle
		if level >= 5 {
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
		} else if level >= 3 {
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#56616F"))
		}
		b.WriteString(style.Render(string(blocks[level])))
	}
	return b.String()
}

func tunnelStatusLabel(t *tunnel.Tunnel) string {
	switch t.Status {
	case tunnel.StatusRunning:
		return "Running"
	case tunnel.StatusError:
		return "Error"
	case tunnel.StatusConnecting:
		return "Connecting"
	case tunnel.StatusStopped:
		return "Stopped"
	default:
		return "-"
	}
}

func tunnelTypeLabel(t tunnel.TunnelType) string {
	return strings.ToUpper(t.String())
}

func statusColor(status string) color.Color {
	switch status {
	case "Running":
		return colorSuccess
	case "Connecting":
		return colorWarning
	case "Error":
		return colorDanger
	case "Stopped":
		return colorMuted
	default:
		return colorAccent
	}
}

func renderTunnelErrorHint(t *tunnel.Tunnel, width int) []string {
	errMsg := t.Error
	switch {
	case strings.Contains(errMsg, "SSH_AUTH_FAILED"):
		return []string{
			dangerStyle.Render("Authentication failed. Check SSH key:"),
			mutedStyle.Render("Ensure valid key in ~/.ssh/id_rsa or ~/.ssh/id_ed25519, or specify IdentityFile in ~/.ssh/config"),
		}
	case strings.Contains(errMsg, "SSH_CONNECTION_FAILED"):
		return []string{dangerStyle.Render("Connection failed:"), mutedStyle.Render(truncate(errMsg, width))}
	case strings.Contains(errMsg, "HOST_KEY_NOT_CACHED"):
		return []string{
			dangerStyle.Render("Host key not cached. Run manually:"),
			selectedStyle.Render(fmt.Sprintf("ssh %s@%s -p %s", t.SSHConfig.User, t.SSHConfig.Host, t.SSHConfig.Port)),
		}
	case strings.Contains(errMsg, "SSH_CONNECTION_LOST"):
		detail := strings.TrimSpace(strings.TrimPrefix(errMsg, "SSH_CONNECTION_LOST:"))
		if detail == "" {
			return []string{dangerStyle.Render("SSH connection lost - press 'r' to reconnect")}
		}
		return []string{
			dangerStyle.Render("SSH connection lost - press 'r' to reconnect"),
			mutedStyle.Render(truncate(detail, width)),
		}
	default:
		return []string{dangerStyle.Render("Error: " + truncate(errMsg, width))}
	}
}
