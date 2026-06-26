package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spance/intun/internal/tunnel"
)

type shortcutCommand struct {
	key   string
	label string
}

func (m Model) renderShortcuts() string {
	width := renderWidth(m.width)

	var items []shortcutCommand
	switch m.screen {
	case ScreenMain:
		items = m.mainShortcutCommands()
	case ScreenSelectHost, ScreenSelectType:
		items = []shortcutCommand{
			{"↑↓", "Navigate"},
			{"Enter", "Select"},
			{"Esc", "Back"},
		}
	case ScreenInputPort:
		items = []shortcutCommand{
			{"0-9", "Input Port"},
			{"Enter", "Confirm"},
			{"Esc", "Back"},
		}
	case ScreenSFTP:
		if m.sftpPreviewing {
			items = []shortcutCommand{
				{"Esc", "Close Preview"},
			}
		} else if m.sftpTransferring {
			items = []shortcutCommand{
				{"Tab", "Switch"},
				{"↑↓", "Navigate"},
				{"●", "Transferring"},
				{"q", "Close"},
			}
		} else {
			items = []shortcutCommand{
				{"Enter", "Open"},
				{"Tab", "Switch"},
				{"↑↓", "Navigate"},
				{"s", "Sync"},
				{"r", "Sync Dir"},
				{"n", "Rename"},
				{"v", "Preview"},
				{"q", "Close"},
			}
		}
	}

	segments := make([]string, 0, len(items))
	for _, item := range items {
		segments = append(segments, commandSegment(item.key, item.label))
	}
	separator := lipgloss.NewStyle().Foreground(colorMuted).Render(" · ")
	body := strings.Join(segments, separator)
	if lipgloss.Width(body) > width {
		body = truncateANSI(body, width)
	}
	return commandBarStyle.Width(width).Render(lipgloss.PlaceHorizontal(width-2, lipgloss.Center, body))
}

func commandSegment(key, label string) string {
	return lipgloss.JoinHorizontal(lipgloss.Center,
		commandKeyStyle.Render(key),
		commandTextStyle.Render(label),
	)
}

func (m Model) mainShortcutCommands() []shortcutCommand {
	items := []shortcutCommand{{"↑↓", "Navigate"}}
	tunnels := m.manager.List()
	if len(tunnels) == 0 || m.selectedIndex < 0 || m.selectedIndex >= len(tunnels) {
		return append(items,
			shortcutCommand{"c", "Create"},
			shortcutCommand{"q", "Quit"},
		)
	}

	switch tunnels[m.selectedIndex].GetStatus() {
	case tunnel.StatusRunning:
		items = append(items,
			shortcutCommand{"f", "SFTP"},
			shortcutCommand{"s", "Stop"},
			shortcutCommand{"r", "Reconnect"},
			shortcutCommand{"d", "Delete"},
		)
	case tunnel.StatusStopped:
		items = append(items,
			shortcutCommand{"s", "Start"},
			shortcutCommand{"d", "Delete"},
		)
	case tunnel.StatusError:
		items = append(items,
			shortcutCommand{"r", "Reconnect"},
			shortcutCommand{"d", "Delete"},
		)
	case tunnel.StatusConnecting:
		items = append(items,
			shortcutCommand{"●", "Connecting"},
			shortcutCommand{"d", "Delete"},
		)
	default:
		items = append(items, shortcutCommand{"d", "Delete"})
	}
	return append(items,
		shortcutCommand{"c", "Create"},
		shortcutCommand{"q", "Quit"},
	)
}
