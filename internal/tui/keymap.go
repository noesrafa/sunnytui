package tui

import "charm.land/bubbles/v2/key"

type KeyMap struct {
	Send          key.Binding
	Newline       key.Binding
	Quit          key.Binding // esc → open quit dialog
	ClearOrCancel key.Binding // single press: clear input; double press: cancel turn
	NewSession    key.Binding
	NextSession   key.Binding
	PrevSession   key.Binding
	CloseSession  key.Binding
	Rename        key.Binding
	ScrollUp      key.Binding
	ScrollDn      key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Send:          key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
		Newline:       key.NewBinding(key.WithKeys("ctrl+j", "alt+enter"), key.WithHelp("ctrl+j", "newline")),
		Quit:          key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "quit")),
		ClearOrCancel: key.NewBinding(key.WithKeys("ctrl+c", "ctrl+d"), key.WithHelp("ctrl+c", "clear/cancel")),
		NewSession:    key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "new")),
		NextSession:   key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next")),
		PrevSession:   key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev")),
		CloseSession:  key.NewBinding(key.WithKeys("ctrl+w"), key.WithHelp("ctrl+w", "close")),
		Rename:        key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "rename")),
		ScrollUp:      key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "scroll up")),
		ScrollDn:      key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "scroll down")),
	}
}
