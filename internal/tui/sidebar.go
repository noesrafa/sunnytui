package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/noesrafa/sunnytui/internal/claude"
	"github.com/noesrafa/sunnytui/internal/session"
)

const sidebarWidth = 30

func renderSidebar(mgr *session.Manager, height int, s Styles) string {
	innerW := sidebarWidth - 4 // padding(0,1) + 1 col safety on each side

	rows := []string{renderLogo(innerW, s), ""}
	rows = append(rows,
		s.HeaderTitle.Render("sessions"),
		s.HeaderSep.Render(strings.Repeat("─", innerW)),
	)
	if len(mgr.Sessions) == 0 {
		rows = append(rows, s.Hint.Render("(none)"))
	}
	for i, sess := range mgr.Sessions {
		if i > 0 {
			rows = append(rows, "")
		}
		rows = append(rows, renderSidebarRow(sess, i == mgr.Active, s)...)
	}

	// Usage widget — derived from the rate_limit_event stream-json events
	// (Claude Code emits one per turn). Shows up below the sessions list.
	if cur := mgr.Current(); cur != nil && cur.RateLimit != nil {
		rows = append(rows, "", s.HeaderTitle.Render("usage"),
			s.HeaderSep.Render(strings.Repeat("─", innerW)))
		rows = append(rows, renderUsage(cur.RateLimit, s)...)
	}

	rows = append(rows, "", s.HeaderSep.Render(strings.Repeat("─", innerW)))
	rows = append(rows,
		s.StatusKey.Render("ctrl+n")+s.Hint.Render(" new"),
		s.StatusKey.Render("ctrl+r")+s.Hint.Render(" rename"),
		s.StatusKey.Render("tab")+s.Hint.Render("    next"),
		s.StatusKey.Render("ctrl+w")+s.Hint.Render(" close"),
		s.StatusKey.Render("esc")+s.Hint.Render("    quit"),
	)

	body := strings.Join(rows, "\n")
	return lipgloss.NewStyle().
		Width(sidebarWidth).
		Height(height).
		Padding(0, 1).
		BorderRight(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colBorder).
		Render(body)
}

func renderSidebarRow(sess *session.Session, active bool, s Styles) []string {
	badge := stateBadge(sess.State, s)
	title := sess.Title
	if title == "" {
		title = "session"
	}
	maxTitleLen := sidebarWidth - 8
	if maxTitleLen > 0 && len(title) > maxTitleLen {
		title = "…" + title[len(title)-(maxTitleLen-1):]
	}
	indicator := " "
	titleStyle := s.AssistantText
	if active {
		indicator = s.UserPrompt.Render("▎")
		titleStyle = s.AssistantText.Bold(true)
	}
	line1 := indicator + badge + " " + titleStyle.Render(title)

	var line2 string
	switch sess.State {
	case session.StateThinking:
		live := sess.LiveStatus()
		secs := time.Since(sess.StartedAt).Seconds()
		txt := fmt.Sprintf("%s · %.1fs", live, secs)
		line2 = "    " + s.StatusBusy.Render(txt)
	case session.StateError:
		msg := "error"
		if sess.LastErr != nil {
			msg = sess.LastErr.Error()
		}
		if len(msg) > sidebarWidth-6 && sidebarWidth > 6 {
			msg = msg[:sidebarWidth-7] + "…"
		}
		line2 = "    " + s.ResultError.Render(msg)
	default:
		if sess.Turns > 0 {
			line2 = "    " + s.Hint.Render(fmt.Sprintf("%d turns", sess.Turns))
		} else {
			line2 = "    " + s.Hint.Render("ready")
		}
	}
	return []string{line1, line2}
}

func renderUsage(rl *claude.RateLimitInfo, s Styles) []string {
	label := rl.RateLimitType
	switch label {
	case "five_hour":
		label = "5h window"
	case "weekly":
		label = "7d window"
	case "":
		label = "rate limit"
	}

	statusStyle := s.StatusIdle
	statusText := "ok"
	if rl.Status != "" && rl.Status != "allowed" {
		statusStyle = s.StatusBusy
		statusText = rl.Status
	}
	line1 := s.AssistantText.Render(label) + " " + statusStyle.Render("●"+" "+statusText)

	var line2 string
	if rl.ResetsAt > 0 {
		d := time.Until(time.Unix(rl.ResetsAt, 0))
		if d < 0 {
			line2 = s.Hint.Render("resetting…")
		} else {
			line2 = s.Hint.Render("resets in " + shortDuration(d))
		}
	}

	rows := []string{line1}
	if line2 != "" {
		rows = append(rows, line2)
	}
	if rl.IsUsingOverage {
		rows = append(rows, s.StatusBusy.Render("⚠ on overage"))
	}
	return rows
}

func shortDuration(d time.Duration) string {
	if d > time.Hour {
		hours := int(d.Hours())
		mins := int(d.Minutes()) - hours*60
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	if d > time.Minute {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

func stateBadge(st session.State, s Styles) string {
	switch st {
	case session.StateThinking:
		return s.StatusBusy.Render("◐")
	case session.StateError:
		return s.ResultError.Render("✗")
	default:
		return s.StatusIdle.Render("●")
	}
}
