package tui

import "github.com/noesrafa/sunnytui/internal/claude"

type sessionEventMsg struct {
	SessionID string
	Event     claude.Event
}

type sessionClosedMsg struct {
	SessionID string
}

// paneOutputMsg is fired by the per-pane PTY-read tea.Cmd. The data has
// already been written into the vt10x emulator inside Pane.ReadOnce, so the
// handler only needs to trigger a re-render and re-arm the next read.
type paneOutputMsg struct {
	PaneID string
}

type paneClosedMsg struct {
	PaneID string
	Err    error
}
