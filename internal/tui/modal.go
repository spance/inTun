package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

type ModalView struct {
	content string
	width   int
}

type ModalSeverity int

const (
	ModalInfo ModalSeverity = iota
	ModalWarning
	ModalDanger
)

type ModalField struct {
	Label string
	Value string
}

type ModalAction struct {
	Key   string
	Label string
}

type ModalSpec struct {
	Title    string
	Severity ModalSeverity
	Body     []string
	Fields   []ModalField
	Actions  []ModalAction
	Width    int
}

func (m ModalView) String() string {
	return m.content
}

func overlayCentered(base string, modal ModalView, width, height int) string {
	if modal.content == "" {
		return base
	}

	width = renderWidth(width)
	height = renderHeight(height)
	lines := make([]string, height)
	for i := range lines {
		lines[i] = strings.Repeat(" ", width)
	}

	modalLines := strings.Split(modal.content, "\n")
	top := (height - len(modalLines)) / 2
	if top < 0 {
		top = 0
	}
	left := (width - modal.width) / 2
	if left < 0 {
		left = 0
	}

	for i, modalLine := range modalLines {
		row := top + i
		if row >= len(lines) {
			break
		}
		lines[row] = modalLineOnMask(modalLine, left, width)
	}

	return strings.Join(lines, "\n")
}

func renderModal(screenWidth int, body string, modalWidth int) ModalView {
	if modalWidth <= 0 {
		modalWidth = min(64, screenWidth-8)
	}
	maxWidth := screenWidth - 4
	if modalWidth > maxWidth {
		modalWidth = maxWidth
	}
	if modalWidth < 40 {
		modalWidth = screenWidth - 4
	}

	style := lipgloss.NewStyle().
		Width(modalWidth).
		Border(uiBorder()).
		BorderForegroundBlend(colorWarning, colorAccent2).
		Padding(1, 2)
	contentWidth := modalWidth - style.GetHorizontalFrameSize()
	if contentWidth < 1 {
		contentWidth = 1
	}
	lines := make([]string, 0, len(strings.Split(body, "\n")))
	for _, line := range strings.Split(body, "\n") {
		lines = append(lines, fitLine(line, contentWidth))
	}
	rendered := style.Render(strings.Join(lines, "\n"))
	return ModalView{
		content: rendered,
		width:   maxLineWidth(rendered),
	}
}

func renderModalSpec(width int, spec ModalSpec) ModalView {
	var b strings.Builder
	b.WriteString(modalTitleStyle(spec.Severity).Render(spec.Title))
	b.WriteString("\n")

	if len(spec.Body) > 0 || len(spec.Fields) > 0 {
		b.WriteString("\n")
	}
	for _, line := range spec.Body {
		if line == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString(selectedStyle.Render(line))
		b.WriteString("\n")
	}
	for _, field := range spec.Fields {
		b.WriteString(accentStyle.Bold(true).Render(field.Label + ": "))
		b.WriteString(field.Value)
		b.WriteString("\n")
	}
	if len(spec.Actions) > 0 {
		b.WriteString("\n")
		b.WriteString(shortcutStyle.Render(renderModalActions(spec.Actions)))
	}

	modalWidth := spec.Width
	if modalWidth <= 0 {
		modalWidth = min(72, width-8)
	}
	if modalWidth < 40 {
		modalWidth = width - 4
	}
	return renderModal(width, strings.TrimRight(b.String(), "\n"), modalWidth)
}

func modalTitleStyle(severity ModalSeverity) lipgloss.Style {
	bg := colorAccent
	fg := colorPanel
	if severity == ModalWarning {
		bg = colorWarning
	}
	if severity == ModalDanger {
		bg = colorDanger
		fg = colorSelected
	}
	return lipgloss.NewStyle().
		Background(bg).
		Foreground(fg).
		Bold(true).
		Padding(0, 1)
}

func renderModalActions(actions []ModalAction) string {
	parts := make([]string, 0, len(actions))
	for _, action := range actions {
		parts = append(parts, "["+action.Key+"] "+action.Label)
	}
	return strings.Join(parts, "    ")
}

func modalMessageParts(msg string) ([]string, []ModalField) {
	var body []string
	var fields []ModalField
	for _, line := range strings.Split(msg, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "FROM:"):
			fields = append(fields, ModalField{Label: "FROM", Value: strings.TrimSpace(strings.TrimPrefix(trimmed, "FROM:"))})
		case strings.HasPrefix(trimmed, "TO:"):
			fields = append(fields, ModalField{Label: "TO", Value: strings.TrimSpace(strings.TrimPrefix(trimmed, "TO:"))})
		default:
			body = append(body, trimmed)
		}
	}
	return body, fields
}

func modalLineOnMask(overlay string, left, width int) string {
	if left >= width {
		return strings.Repeat(" ", width)
	}
	overlayWidth := lipgloss.Width(overlay)
	if overlayWidth <= 0 {
		return strings.Repeat(" ", width)
	}
	if left+overlayWidth > width {
		overlay = truncateANSI(overlay, width-left)
		overlayWidth = lipgloss.Width(overlay)
	}
	right := width - left - overlayWidth
	if right < 0 {
		right = 0
	}
	return strings.Repeat(" ", left) + overlay + strings.Repeat(" ", right)
}

func maxLineWidth(content string) int {
	maxWidth := 0
	for _, line := range strings.Split(content, "\n") {
		if w := lipgloss.Width(line); w > maxWidth {
			maxWidth = w
		}
	}
	return maxWidth
}
