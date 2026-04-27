package tui

import (
	tea "charm.land/bubbletea/v2"
)

// Dialog is a modal that takes focus over the main UI. The root model routes
// keyboard messages to the top-of-stack dialog first. Dialogs communicate
// results by emitting tea.Msgs that the root model handles below.
type Dialog interface {
	Init() tea.Cmd
	Update(msg tea.Msg) tea.Cmd
	View(width, height int) string
}

type Overlay struct {
	stack []Dialog
}

func (o *Overlay) Open(d Dialog) tea.Cmd {
	o.stack = append(o.stack, d)
	return d.Init()
}

func (o *Overlay) CloseTop() {
	if len(o.stack) == 0 {
		return
	}
	o.stack = o.stack[:len(o.stack)-1]
}

func (o *Overlay) HasOpen() bool { return len(o.stack) > 0 }

func (o *Overlay) Top() Dialog {
	if len(o.stack) == 0 {
		return nil
	}
	return o.stack[len(o.stack)-1]
}

func (o *Overlay) UpdateTop(msg tea.Msg) tea.Cmd {
	if len(o.stack) == 0 {
		return nil
	}
	return o.stack[len(o.stack)-1].Update(msg)
}

func (o *Overlay) ViewTop(width, height int) string {
	if len(o.stack) == 0 {
		return ""
	}
	return o.stack[len(o.stack)-1].View(width, height)
}

// CloseDialogMsg dismisses the top dialog.
type CloseDialogMsg struct{}

// CreateSessionMsg requests the root model to spawn a new session at cwd.
type CreateSessionMsg struct {
	Cwd    string
	Model  string
	Effort string
}

// RenameSessionMsg requests the root model to rename the current session.
type RenameSessionMsg struct {
	NewTitle string
}

// ConfirmQuitMsg signals the root model to terminate the program.
type ConfirmQuitMsg struct{}
