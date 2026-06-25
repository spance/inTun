package tui

import "charm.land/lipgloss/v2"

type shortcutCommand struct {
	key   string
	label string
}

func (m Model) renderShortcuts() string {
	width := renderWidth(m.width)

	var items []shortcutCommand
	switch m.screen {
	case ScreenMain:
		items = []shortcutCommand{
			{"↑↓", "Navigate"},
			{"c", "Create"},
			{"f", "SFTP"},
			{"r", "Reconnect"},
			{"s", "Stop/Start"},
			{"d", "Delete"},
			{"q", "Quit"},
		}
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
				{"q", "Back"},
			}
		} else {
			items = []shortcutCommand{
				{"Tab", "Switch"},
				{"↑↓", "Navigate"},
				{"Enter", "Open"},
				{"s", "Sync"},
				{"r", "Sync Dir"},
				{"n", "Rename"},
				{"v", "Preview"},
				{"q", "Back"},
			}
		}
	}

	segments := make([]string, 0, len(items))
	for _, item := range items {
		segments = append(segments, commandSegment(item.key, item.label))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Center, segments...)
	if lipgloss.Width(body) > width {
		body = truncateANSI(body, width)
	}
	return commandBarStyle.Width(width).Render(lipgloss.PlaceHorizontal(width-2, lipgloss.Center, body))
}

func commandSegment(key, label string) string {
	return lipgloss.JoinHorizontal(lipgloss.Center,
		commandKeyStyle.Render(key),
		commandTextStyle.Render(label),
		lipgloss.NewStyle().Foreground(colorMuted).PaddingRight(1).Render("·"),
	)
}
