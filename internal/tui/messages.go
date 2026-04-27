package tui

import (
	"github.com/noesrafa/sunnytui/internal/claude"
	"github.com/noesrafa/sunnytui/internal/sysstats"
)

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

// branchTickMsg is fired every few seconds so the input-hint row can pick up
// branch changes (e.g. user ran `git checkout` in another terminal).
type branchTickMsg struct{}

// logoTickMsg drives the SUNNY-letters gradient sweep. One per ~120ms is
// fast enough to read as motion and slow enough to be unobtrusive on
// long-running sessions.
type logoTickMsg struct{}

// sysStatsTickMsg requests a fresh CPU/RAM sample. The handler returns a
// sysStatsResultMsg once `top` has come back with output.
type sysStatsTickMsg struct{}

// sysStatsResultMsg carries one snapshot from sysstats.Sample.
type sysStatsResultMsg struct {
	Stats sysstats.Stats
}
