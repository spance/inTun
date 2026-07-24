package tui

import (
	"fmt"
	"strings"
)

func (m Model) renderHostSelect() string {
	width := renderWidth(m.width)
	visibleItems := hostSelectVisibleItems(m.height)
	m.hostScroll = clampScroll(m.hostCursor, m.hostScroll, visibleItems)
	hosts := m.filteredHosts()

	var b strings.Builder
	title := "Select Host"
	if len(hosts) > 0 {
		end := min(len(hosts), m.hostScroll+visibleItems)
		title = fmt.Sprintf("Select Host  %d-%d/%d", m.hostScroll+1, end, len(hosts))
	}
	b.WriteString(sectionTitleStyle.Render(title))
	b.WriteString("\n")
	if m.hostFiltering {
		b.WriteString(m.hostFilter.View())
		b.WriteString("\n")
	}
	if len(hosts) == 0 {
		empty := "No matching hosts"
		if len(m.hosts) == 0 {
			empty = "No SSH config hosts. Press m to enter one manually."
		}
		b.WriteString(panelStyle(width-2, 5, false).Render(mutedStyle.Render(empty)))
		b.WriteString("\n")
		return b.String()
	}
	end := min(len(hosts), m.hostScroll+visibleItems)
	for i := m.hostScroll; i < end; i++ {
		h := hosts[i]
		name := h.Name
		if name == "" {
			name = h.Hostname
		}
		title := safeInline(name)
		if len(h.Labels) > 0 {
			title += "  "
		}
		desc := safeInline(fmt.Sprintf("%s@%s:%s", h.User, h.Hostname, h.Port))
		renderedTitle := selectedStyle.Render(truncate(title, width-4))
		for _, label := range h.Labels {
			renderedTitle += labelPillStyle.Render(safeInline(label))
		}
		row := renderedTitle + "\n" + mutedStyle.Render(truncate(desc, width-4))
		if i == m.hostCursor {
			b.WriteString(listSelectedStyle.Width(width - 2).Render(row))
		} else {
			b.WriteString(listItemStyle.Width(width - 2).Render(row))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) renderManualHostInput() string {
	width := renderWidth(m.width)
	var b strings.Builder
	b.WriteString(sectionTitleStyle.Render("Connect to SSH Host"))
	b.WriteString("\n\n")
	b.WriteString(panelStyle(max(1, width-2), 5, true).Render(
		m.manualHostInput.View() + "\n\n" +
			mutedStyle.Render("Examples: admin@example.com · admin@example.com:2222 · admin@[2001:db8::1]:22"),
	))
	return b.String()
}
