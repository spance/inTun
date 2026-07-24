package tui

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func newTextInput(placeholder string) textinput.Model {
	input := textinput.New()
	input.Placeholder = placeholder
	input.CharLimit = 512
	input.SetWidth(48)
	styles := textinput.DefaultDarkStyles()
	styles.Focused.Text = lipgloss.NewStyle().Foreground(colorSelected)
	styles.Focused.Placeholder = lipgloss.NewStyle().Foreground(colorMuted)
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styles.Blurred.Text = lipgloss.NewStyle().Foreground(colorText)
	styles.Blurred.Placeholder = lipgloss.NewStyle().Foreground(colorMuted)
	styles.Cursor.Color = colorAccent
	styles.Cursor.Shape = tea.CursorBar
	styles.Cursor.Blink = true
	input.SetStyles(styles)
	return input
}

func newComponentState() componentState {
	helpModel := help.New()
	helpModel.ShortSeparator = " · "
	helpModel.Styles.ShortKey = lipgloss.NewStyle().
		Background(colorAccent).
		Foreground(colorPanel).
		Bold(true).
		Padding(0, 1)
	helpModel.Styles.ShortDesc = lipgloss.NewStyle().
		Foreground(colorText).
		PaddingRight(1)
	helpModel.Styles.ShortSeparator = lipgloss.NewStyle().Foreground(colorMuted)
	helpModel.Styles.Ellipsis = lipgloss.NewStyle().Foreground(colorMuted)

	tunnelViewport := viewport.New(
		viewport.WithWidth(defaultWidth),
		viewport.WithHeight(defaultHeight-10),
	)
	tunnelViewport.FillHeight = false
	tunnelViewport.MouseWheelEnabled = true
	return componentState{
		spinner: spinner.New(
			spinner.WithSpinner(spinner.MiniDot),
			spinner.WithStyle(lipgloss.NewStyle().Foreground(colorAccent)),
		),
		tunnelViewport: tunnelViewport,
		help:           helpModel,
	}
}

func (m Model) resizeComponents(width, height int) Model {
	inputWidth := max(12, min(56, width-8))
	m.hostFilter.SetWidth(inputWidth)
	m.manualHostInput.SetWidth(inputWidth)
	m.help.SetWidth(max(1, width-2))
	m.tunnelViewport.SetWidth(max(1, width-2))
	m.tunnelViewport.SetHeight(max(4, height-10))
	return m
}
