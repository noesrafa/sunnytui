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
	Runs          key.Binding // open the runs manager modal
	NewPane       key.Binding // open the new-terminal-pane dialog
	TilePicker    key.Binding // ctrl+k — searchable tab switcher
	Settings      key.Binding // ctrl+s — open settings modal (theme picker)
	ScrollUp      key.Binding
	ScrollDn      key.Binding
	Paste         key.Binding // ctrl+v — image-aware paste (image first, then text)
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
		Runs:          key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "runs")),
		NewPane:       key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "new term")),
		TilePicker:    key.NewBinding(key.WithKeys("ctrl+k"), key.WithHelp("ctrl+k", "switch tab")),
		Settings:      key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "settings")),
		ScrollUp:      key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "scroll up")),
		ScrollDn:      key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "scroll down")),
		Paste:         key.NewBinding(key.WithKeys("ctrl+v"), key.WithHelp("ctrl+v", "paste image/text")),
	}
}
