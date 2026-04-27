package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/noesrafa/sunnytui/internal/claude"
	"github.com/noesrafa/sunnytui/internal/runs"
	"github.com/noesrafa/sunnytui/internal/session"
	"github.com/noesrafa/sunnytui/internal/terminal"
	"github.com/noesrafa/sunnytui/internal/usage"
)

const (
	sidebarWidth = 30
	sidebarGap   = 3 // empty cols between sidebar and main column
)

func renderSidebar(mgr *session.Manager, runMgr *runs.Manager, paneMgr *terminal.Manager, activePaneActive bool, height int, s Styles) string {
	innerW := sidebarWidth - 4 // padding(0,1) + 1 col safety on each side

	rows := []string{renderLogo(innerW, s), ""}
	rows = append(rows, renderSessionsSection(mgr, activePaneActive, innerW, s)...)
	if section := renderTermsSection(paneMgr, activePaneActive, innerW, s); len(section) > 0 {
		rows = append(rows, "")
		rows = append(rows, section...)
	}
	if section := renderUsageSection(mgr, innerW, s); len(section) > 0 {
		rows = append(rows, "")
		rows = append(rows, section...)
	}
	if section := renderRunsSection(runMgr, innerW, s); len(section) > 0 {
		rows = append(rows, "")
		rows = append(rows, section...)
	}
	rows = append(rows, "")
	rows = append(rows, renderShortcutsSection(innerW, s)...)

	body := strings.Join(rows, "\n")
	return lipgloss.NewStyle().
		Width(sidebarWidth).
		Height(height).
		Padding(0, 1).
		Render(body)
}

// sectionHeader returns the bold title + the rule below it, the canonical
// way every sidebar block starts.
func sectionHeader(title string, innerW int, s Styles) []string {
	return []string{
		s.HeaderTitle.Render(title),
		s.HeaderSep.Render(strings.Repeat("─", innerW)),
	}
}

func renderSessionsSection(mgr *session.Manager, activePaneActive bool, innerW int, s Styles) []string {
	rows := sectionHeader("sessions", innerW, s)
	if len(mgr.Sessions) == 0 {
		return append(rows, s.Hint.Render("(none)"))
	}
	for i, sess := range mgr.Sessions {
		if i > 0 {
			rows = append(rows, "")
		}
		rows = append(rows, renderSidebarRow(sess, !activePaneActive && i == mgr.Active, s)...)
	}
	return rows
}

func renderTermsSection(paneMgr *terminal.Manager, activePaneActive bool, innerW int, s Styles) []string {
	if paneMgr == nil || paneMgr.Len() == 0 {
		return nil
	}
	rows := sectionHeader("terminals", innerW, s)
	for i, p := range paneMgr.Panes {
		rows = append(rows, renderPaneRow(p, activePaneActive && i == paneMgr.Active, s))
	}
	return rows
}

// renderUsageSection prefers the statusline snapshot (has percentages,
// populated when `sunnytui statusline` is registered with Claude Code) and
// falls back to the rate_limit_event from stream-json (only has status +
// reset time).
func renderUsageSection(mgr *session.Manager, innerW int, s Styles) []string {
	usageRows := buildUsageWidget(mgr, innerW, s)
	if len(usageRows) == 0 {
		return nil
	}
	return append(sectionHeader("usage", innerW, s), usageRows...)
}

// renderRunsSection always shows once a manager exists, so the "press ctrl+u"
// hint surface is discoverable even with zero registered runs.
func renderRunsSection(runMgr *runs.Manager, innerW int, s Styles) []string {
	if runMgr == nil {
		return nil
	}
	rows := sectionHeader("runs", innerW, s)
	all := runMgr.All()
	if len(all) == 0 {
		return append(rows, s.Hint.Render("(none)"))
	}
	for _, r := range all {
		rows = append(rows, renderRunSummary(r, innerW, s))
	}
	return rows
}

func renderShortcutsSection(innerW int, s Styles) []string {
	return []string{
		s.HeaderSep.Render(strings.Repeat("─", innerW)),
		s.StatusKey.Render("ctrl+n") + s.Hint.Render(" new chat"),
		s.StatusKey.Render("ctrl+t") + s.Hint.Render(" new term"),
		s.StatusKey.Render("ctrl+r") + s.Hint.Render(" rename"),
		s.StatusKey.Render("ctrl+u") + s.Hint.Render(" runs"),
		s.StatusKey.Render("ctrl+k") + s.Hint.Render(" switch"),
		s.StatusKey.Render("ctrl+s") + s.Hint.Render(" select"),
		s.StatusKey.Render("tab") + s.Hint.Render("    next"),
		s.StatusKey.Render("ctrl+w") + s.Hint.Render(" close"),
		s.StatusKey.Render("esc") + s.Hint.Render("    quit"),
	}
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

// buildUsageWidget tries the rich percentage view first (statusline snapshot)
// and falls back to a status-only line from the in-stream rate_limit_event.
func buildUsageWidget(mgr *session.Manager, innerW int, s Styles) []string {
	if payload, _, err := usage.Read(10 * time.Minute); err == nil && payload != nil && payload.RateLimits != nil {
		var rows []string
		if w := payload.RateLimits.FiveHour; w != nil {
			rows = append(rows, renderUsageBar("5h", w, innerW, s)...)
		}
		if w := payload.RateLimits.SevenDay; w != nil {
			if len(rows) > 0 {
				rows = append(rows, "")
			}
			rows = append(rows, renderUsageBar("7d", w, innerW, s)...)
		}
		if len(rows) > 0 {
			return rows
		}
	}
	if cur := mgr.Current(); cur != nil && cur.RateLimit != nil {
		return renderUsage(cur.RateLimit, s)
	}
	return nil
}

// renderUsageBar paints a claude-hud-style row:
//
//	5h ███░░░░░ 25%
//	   resets in 1h 30m
func renderUsageBar(label string, w *usage.Window, innerW int, s Styles) []string {
	pct := w.UsedPercentage
	barW := innerW - len(label) - 6 // " " + bar + " NN%"
	if barW < 6 {
		barW = 6
	}
	filled := pct * barW / 100
	if filled < 0 {
		filled = 0
	}
	if filled > barW {
		filled = barW
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barW-filled)

	barStyle := s.StatusIdle
	switch {
	case pct >= 90:
		barStyle = s.ResultError
	case pct >= 70:
		barStyle = s.StatusBusy
	}
	header := s.HeaderDim.Render(label) + " " + barStyle.Render(bar) + " " +
		barStyle.Render(fmt.Sprintf("%d%%", pct))

	rows := []string{header}
	if w.ResetsAt > 0 {
		d := time.Until(time.Unix(w.ResetsAt, 0))
		if d < 0 {
			rows = append(rows, "   "+s.Hint.Render("resetting…"))
		} else {
			rows = append(rows, "   "+s.Hint.Render("resets in "+shortDuration(d)))
		}
	}
	return rows
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

func renderPaneRow(p *terminal.Pane, active bool, s Styles) string {
	var icon string
	if p.Alive() {
		icon = s.StatusIdle.Render("▶")
	} else {
		icon = s.Hint.Render("□")
	}
	indicator := " "
	titleStyle := s.AssistantText
	if active {
		indicator = s.UserPrompt.Render("▎")
		titleStyle = s.AssistantText.Bold(true)
	}
	name := p.Title
	maxLen := sidebarWidth - 8
	if len(name) > maxLen && maxLen > 0 {
		name = name[:maxLen-1] + "…"
	}
	return indicator + icon + " " + titleStyle.Render(name)
}

func renderRunSummary(r *runs.Run, innerW int, s Styles) string {
	var icon string
	switch r.Status {
	case runs.StatusRunning:
		icon = s.StatusIdle.Render("●")
	case runs.StatusCrashed:
		icon = s.ResultError.Render("✗")
	default:
		icon = s.Hint.Render("○")
	}
	name := r.Name
	maxName := innerW - 6
	if maxName < 4 {
		maxName = 4
	}
	if len(name) > maxName {
		name = name[:maxName-1] + "…"
	}
	return " " + icon + " " + s.AssistantText.Render(name)
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
