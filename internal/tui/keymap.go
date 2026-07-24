package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

type sftpKeyMap struct {
	Close    key.Binding
	Switch   key.Binding
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Open     key.Binding
	Sync     key.Binding
	SyncDir  key.Binding
	Preview  key.Binding
	Rename   key.Binding
	Cancel   key.Binding
	Confirm  key.Binding
}

var defaultSFTPKeyMap = sftpKeyMap{
	Close: key.NewBinding(
		key.WithKeys("q", "esc"),
		key.WithHelp("q/esc", "close"),
	),
	Switch: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "switch"),
	),
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("up/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("down/j", "down"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("pgup"),
		key.WithHelp("pgup", "page up"),
	),
	PageDown: key.NewBinding(
		key.WithKeys("pgdown"),
		key.WithHelp("pgdn", "page down"),
	),
	Open: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "open"),
	),
	Sync: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "sync"),
	),
	SyncDir: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "sync dir"),
	),
	Preview: key.NewBinding(
		key.WithKeys("v"),
		key.WithHelp("v", "preview"),
	),
	Rename: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "rename"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	),
	Confirm: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "confirm"),
	),
}

func matchKey(msg tea.KeyMsg, binding key.Binding) bool {
	return key.Matches(msg, binding)
}
