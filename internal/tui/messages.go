package tui

import "github.com/noesrafa/sunnytui/internal/claude"

type sessionEventMsg struct {
	SessionID string
	Event     claude.Event
}

type sessionClosedMsg struct {
	SessionID string
}

type sessionCreateErrMsg struct {
	Err error
}
