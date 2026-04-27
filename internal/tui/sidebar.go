package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/noesrafa/sunnytui/internal/claude"
	"github.com/noesrafa/sunnytui/internal/runs"
	"github.com/noesrafa/sunnytui/internal/session"
	"github.com/noesrafa/sunnytui/internal/sysstats"
	"github.com/noesrafa/sunnytui/internal/terminal"
	"github.com/noesrafa/sunnytui/internal/usage"
)

const (
	sidebarWidth = 30
	sidebarGap   = 3 // empty cols between sidebar and main column
)

func renderSidebar(mgr *session.Manager, runMgr *runs.Manager, paneMgr *terminal.Manager, activePaneActive bool, height int, s Styles, logoFrame int, sys sysstats.Stats) string {
	innerW := sidebarWidth - 4 // padding(0,1) + 1 col safety on each side

	rows := []string{renderLogo(innerW, s, logoFrame), ""}
	rows = append(rows, renderSessionsSection(mgr, activePaneActive, innerW, s)...)
	if section := renderTermsSection(paneMgr, activePaneActive, innerW, s); len(section) > 0 {
		rows = append(rows, "")
		rows = append(rows, section...)
	}
	if section := renderRunsSection(runMgr, innerW, s); len(section) > 0 {
		rows = append(rows, "")
		rows = append(rows, section...)
	}
	if section := renderUsageSection(mgr, sys, innerW, s); len(section) > 0 {
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
func renderUsageSection(mgr *session.Manager, sys sysstats.Stats, innerW int, s Styles) []string {
	usageRows := buildUsageWidget(mgr, innerW, s)
	sysRows := buildSysStatsRows(sys, innerW, s)
	if len(usageRows) == 0 && len(sysRows) == 0 {
		return nil
	}
	header := sectionHeader("usage", innerW, s)
	body := append([]string{}, usageRows...)
	if len(usageRows) > 0 && len(sysRows) > 0 {
		body = append(body, "") // visual gap between Claude usage and machine usage
	}
	body = append(body, sysRows...)
	return append(header, body...)
}

// buildSysStatsRows renders the whole-machine cpu/ram bars under the
// usage section. Sample==zero (e.g. sysstats.Sample failed or returned
// before the first tick landed) means the section is rendered without
// these rows — easier than threading "is initialized" through everything.
func buildSysStatsRows(st sysstats.Stats, innerW int, s Styles) []string {
	if st.CPUPct == 0 && st.MemPct == 0 {
		return nil
	}
	return []string{
		renderProgressBar("cpu", st.CPUPct, "", innerW, s),
		renderProgressBar("ram", st.MemPct, "", innerW, s),
	}
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
		s.StatusKey.Render("ctrl+s") + s.Hint.Render(" settings"),
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
// Mirrors claude-hud's display: context window % + 5h + 7d rate-limit windows.
//
// Freshness window: 24h. We deliberately keep stale snapshots visible
// instead of disappearing the bars whenever the user steps away from
// claude-hud — Claude Code only refreshes the statusline payload on
// activity, so a 10-minute cutoff hides the widget for the rest of the
// day after a single break.
func buildUsageWidget(mgr *session.Manager, innerW int, s Styles) []string {
	if payload, _, err := usage.Read(24 * time.Hour); err == nil && payload != nil {
		var rows []string
		if cw := payload.ContextWindow; cw != nil && cw.UsedPercentage > 0 {
			rows = append(rows, renderProgressBar("ctx", cw.UsedPercentage, "", innerW, s))
		}
		if rl := payload.RateLimits; rl != nil {
			if w := rl.FiveHour; w != nil {
				rows = append(rows, renderProgressBar("5h", w.UsedPercentage, resetHint(w.ResetsAt), innerW, s))
			}
			if w := rl.SevenDay; w != nil {
				rows = append(rows, renderProgressBar("7d", w.UsedPercentage, resetHint(w.ResetsAt), innerW, s))
			}
		}
		if len(rows) > 0 {
			return rows
		}
	}
	if cur := mgr.Current(); cur != nil && cur.RateLimit != nil {
		return renderRateLimitFallback(cur.RateLimit, innerW, s)
	}
	return nil
}

// renderRateLimitFallback paints the in-stream rate_limit_event in the
// same compact one-line style as the snapshot bars. We don't have a
// percentage in this path, so the bar is replaced by a status pill +
// reset hint:
//
//	5h ● ok · 55m
//	7d ● ok · 156h
func renderRateLimitFallback(rl *claude.RateLimitInfo, innerW int, s Styles) []string {
	label := "5h"
	switch rl.RateLimitType {
	case "weekly":
		label = "7d"
	case "five_hour", "":
		label = "5h"
	}
	dot := s.StatusIdle.Render("●")
	statusText := "ok"
	if rl.Status != "" && rl.Status != "allowed" {
		dot = s.StatusBusy.Render("●")
		statusText = rl.Status
	}
	parts := []string{
		s.HeaderDim.Render(fmt.Sprintf("%-3s", label)),
		dot,
		s.HeaderDim.Render(statusText),
	}
	if rs := resetHint(rl.ResetsAt); rs != "" {
		parts = append(parts, s.Hint.Render("· "+rs))
	}
	rows := []string{strings.Join(parts, " ")}
	if rl.IsUsingOverage {
		rows = append(rows, "    "+s.StatusBusy.Render("⚠ overage"))
	}
	_ = innerW // reserved for future bar-fitting; not used in fallback today
	return rows
}

// renderProgressBar is the canonical thin one-liner used by every usage
// metric. Layout:
//
//	ctx ━━━━━──────── 15%
//	5h  ━━━━────────  9% 3h54m
//	7d  ─────────────  1% 156h
//
// The filled portion uses a Blend1D ramp from `colTertiary` (mint, healthy)
// to `colDanger` (red, near-cap), so the warmer colors only appear as the
// bar fills towards the right — visually communicates risk without needing
// per-percentage thresholds.
func renderProgressBar(label string, pctF float64, reset string, innerW int, s Styles) string {
	if pctF < 0 {
		pctF = 0
	}
	if pctF > 100 {
		pctF = 100
	}
	pct := int(pctF + 0.5)
	pctStr := fmt.Sprintf("%3d%%", pct)
	paddedLabel := fmt.Sprintf("%-3s", label)

	barW := innerW - lipgloss.Width(paddedLabel) - 1 - 1 - lipgloss.Width(pctStr)
	if reset != "" {
		barW -= 1 + lipgloss.Width(reset)
	}
	if barW < 4 {
		barW = 4
	}

	filled := pct * barW / 100
	if filled < 0 {
		filled = 0
	}
	if filled > barW {
		filled = barW
	}

	var bar strings.Builder
	if barW > 0 {
		ramp := lipgloss.Blend1D(barW, colTertiary, colDanger)
		emptyStyle := lipgloss.NewStyle().Foreground(colBorder)
		for i := 0; i < barW; i++ {
			if i < filled {
				bar.WriteString(lipgloss.NewStyle().Foreground(ramp[i]).Render("━"))
			} else {
				bar.WriteString(emptyStyle.Render("─"))
			}
		}
	}

	line := s.HeaderDim.Render(paddedLabel) + " " + bar.String() + " " + s.HeaderDim.Render(pctStr)
	if reset != "" {
		line += " " + s.Hint.Render(reset)
	}
	return line
}

// resetHint formats an absolute reset timestamp into a compact relative
// duration ("3h54m", "12m"), or "" when the timestamp is missing / past.
func resetHint(resetsAt int64) string {
	if resetsAt <= 0 {
		return ""
	}
	d := time.Until(time.Unix(resetsAt, 0))
	if d <= 0 {
		return ""
	}
	return shortDuration(d)
}

func shortDuration(d time.Duration) string {
	if d >= 24*time.Hour {
		// Long resets (7d window) — collapse to whole hours, no minutes.
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	if d >= time.Hour {
		hours := int(d.Hours())
		mins := int(d.Minutes()) - hours*60
		if mins == 0 {
			return fmt.Sprintf("%dh", hours)
		}
		return fmt.Sprintf("%dh%dm", hours, mins)
	}
	if d >= time.Minute {
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
