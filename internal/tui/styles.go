package tui

import "charm.land/lipgloss/v2"

var (
	colorSurface   = lipgloss.Color("#161B22")
	colorPanel     = lipgloss.Color("#0D1117")
	colorPanelHi   = lipgloss.Color("#1F2937")
	colorGlass     = lipgloss.Color("#111827")
	colorText      = lipgloss.Color("#D6DEEB")
	colorMuted     = lipgloss.Color("#7D8590")
	colorAccent    = lipgloss.Color("#56B6C2")
	colorAccent2   = lipgloss.Color("#7C3AED")
	colorSuccess   = lipgloss.Color("#3FB950")
	colorWarning   = lipgloss.Color("#D29922")
	colorDanger    = lipgloss.Color("#F85149")
	colorDangerDim = lipgloss.Color("#5A2328")
	colorSelected  = lipgloss.Color("#E6EDF3")

	titleStyle = lipgloss.NewStyle().
			Foreground(colorSelected).
			Background(colorAccent2).
			Bold(true).
			Padding(0, 1)

	errorStyle = lipgloss.NewStyle().
			Foreground(colorDanger)

	headerStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Bold(true)

	selectedStyle = lipgloss.NewStyle().
			Foreground(colorSelected).
			Bold(true)

	shortcutStyle = lipgloss.NewStyle().
			Foreground(colorText)

	lineStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	dirStyle = lipgloss.NewStyle().
			Foreground(colorAccent)

	inactiveStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	accentStyle = lipgloss.NewStyle().
			Foreground(colorAccent)

	successStyle = lipgloss.NewStyle().
			Foreground(colorSuccess)

	warningStyle = lipgloss.NewStyle().
			Foreground(colorWarning)

	dangerStyle = lipgloss.NewStyle().
			Foreground(colorDanger)

	mutedStyle = lipgloss.NewStyle().
			Foreground(colorMuted)

	sectionTitleStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true).
				Padding(0, 1)

	eyebrowStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Bold(true)

	listItemStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Padding(0, 1).
			MarginBottom(1)

	listSelectedStyle = lipgloss.NewStyle().
				Foreground(colorSelected).
				Background(colorPanelHi).
				Bold(true).
				Padding(0, 1).
				MarginBottom(1)

	commandBarStyle = lipgloss.NewStyle().
			Background(colorSurface).
			Foreground(colorText).
			Padding(0, 1)

	labelPillStyle = lipgloss.NewStyle().
			Foreground(colorPanel).
			Background(colorWarning).
			Bold(true).
			Padding(0, 1).
			MarginRight(1)
)

func uiBorder() lipgloss.Border {
	return lipgloss.RoundedBorder()
}

func panelStyle(width, height int, focused bool) lipgloss.Style {
	style := lipgloss.NewStyle().
		Width(width).
		Border(uiBorder()).
		BorderForeground(colorSurface).
		Padding(0, 1)
	if height > 0 {
		style = style.Height(height)
	}
	if focused {
		style = style.BorderForeground(colorAccent)
	}
	return style
}
