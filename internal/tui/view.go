package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/noesrafa/sunnytui/internal/session"
	"github.com/noesrafa/sunnytui/internal/terminal"
)

func (m Model) View() tea.View {
	v := tea.NewView("starting…")
	v.AltScreen = true
	// When the user toggles select mode (ctrl+s), drop mouse capture so the
	// host terminal can do its native click-and-drag text selection. Wheel
	// scroll inside the chat stops working in this state — but you can still
	// scroll with pgup/pgdn. Toggle off (ctrl+s again) to restore wheel.
	if m.selectMode {
		v.MouseMode = tea.MouseModeNone
	} else {
		v.MouseMode = tea.MouseModeCellMotion
	}
	if !m.ready {
		return v
	}
	base := m.renderBase()
	if m.overlay.HasOpen() {
		v.SetContent(m.composeWithModal(base))
	} else {
		v.SetContent(base)
		// In claude-session mode the textarea owns its own (virtual) cursor.
		// In pane mode we project the embedded child's caret to absolute
		// screen coordinates so the user sees a real terminal cursor.
		if p := m.activePane(); p != nil {
			cx, cy, vis := p.Cursor()
			if vis {
				// renderBase JoinVerticals (header, body, status). header is
				// "" but `strings.Split("", "\n")` yields [""] — that
				// becomes one blank row before the body. Account for it
				// here so the caret lands on the same row as the cell
				// the child terminal is rendering.
				v.Cursor = tea.NewCursor(
					sidebarWidth+sidebarGap+cx,
					headerHeight+cy,
				)
				v.Cursor.Color = colSecondary
				v.Cursor.Shape = tea.CursorBlock
				v.Cursor.Blink = true
			}
		}
	}
	return v
}


// renderBase produces the full-screen UI without any modal on top.
func (m Model) renderBase() string {
	header := m.renderHeader()
	body := m.renderBody()
	status := m.renderStatus()
	out := lipgloss.JoinVertical(lipgloss.Left, header, body, status)
	return clampHeight(out, m.height)
}

// composeWithModal places the active dialog on top of the base UI using
// lipgloss v2's Canvas + Compositor. Cells the modal doesn't cover keep
// showing the chat underneath — this is the Crush "transparent overlay" feel.
//
// Important: we MUST go through a Compositor (not Canvas.Compose(layer)
// directly), because Compose called on a bare Layer ignores X/Y and draws
// the layer's content over the full canvas bounds.
func (m Model) composeWithModal(base string) string {
	maxW := m.width - 4
	maxH := m.height - 4
	if maxW < 20 {
		maxW = 20
	}
	if maxH < 6 {
		maxH = 6
	}
	modal := m.overlay.ViewTop(maxW, maxH)
	modalW, modalH := lipgloss.Size(modal)

	x := (m.width - modalW) / 2
	if x < 0 {
		x = 0
	}
	y := (m.height - modalH) / 2
	if y < 0 {
		y = 0
	}

	baseLayer := lipgloss.NewLayer(base)
	modalLayer := lipgloss.NewLayer(modal).X(x).Y(y).Z(10)

	canvas := lipgloss.NewCanvas(m.width, m.height)
	canvas.Compose(lipgloss.NewCompositor(baseLayer, modalLayer))
	return canvas.Render()
}

func clampHeight(s string, maxLines int) string {
	if maxLines <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderHeader() string {
	// Header used to show "☀ sunnytui · <session title>" but the logo is
	// already in the sidebar and the title is in the session list — so we
	// leave the header blank to avoid duplication. Keeping the function
	// (returns "") so the layout math (headerHeight=1) stays consistent.
	return ""
}

func (m Model) renderBody() string {
	bodyH := m.height - headerHeight - statusHeight
	if bodyH < 6 {
		bodyH = 6
	}
	main := m.renderMain(bodyH)
	sidebar := renderSidebar(m.manager, m.runs, m.panes, m.activeKind == activePane, bodyH, m.styles)
	// 3-col gap between sidebar and main — Crush-style breathing room, no
	// vertical divider line.
	gap := lipgloss.NewStyle().Width(sidebarGap).Height(bodyH).Render("")
	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, gap, main)
}

func (m Model) renderMain(height int) string {
	mainW := m.width - sidebarWidth - sidebarGap
	if mainW < 20 {
		mainW = 20
	}

	// Pane mode: full main column is the vt10x grid.
	if p := m.activePane(); p != nil {
		return lipgloss.NewStyle().Width(mainW).Height(height).Render(terminal.Render(p))
	}

	// Claude session mode: transcript + input + hint.
	cur := m.manager.Current()
	transcript := m.viewport.View()
	input := m.renderInput(cur)
	hint := m.renderInputHint()
	body := lipgloss.JoinVertical(lipgloss.Left, transcript, input, hint)
	return lipgloss.NewStyle().Width(mainW).Height(height).Render(body)
}

func (m Model) renderInput(cur *session.Session) string {
	style := m.styles.Input
	if cur != nil && cur.State == session.StateIdle {
		style = m.styles.InputFocused
	}
	mainW := m.width - sidebarWidth - sidebarGap
	return style.Width(mainW).Render(m.textarea.View())
}

// renderInputHint is the row under the input. Crush-style: shows the active
// session's cwd · model · branch (shortcuts already live in the sidebar).
func (m Model) renderInputHint() string {
	s := m.styles
	cur := m.manager.Current()
	if cur == nil {
		return ""
	}
	var parts []string
	parts = append(parts, s.HeaderDim.Render(prettyPath(cur.Cwd)))
	if cur.Model != "" {
		parts = append(parts, s.HeaderDim.Render(cur.Model))
	}
	if cur.Branch != "" {
		parts = append(parts, s.HeaderDim.Render("⌥ "+cur.Branch))
	}
	sep := s.HeaderSep.Render(" · ")
	return " " + strings.Join(parts, sep)
}

func (m Model) renderStatus() string {
	totalTurns := 0
	for _, s := range m.manager.Sessions {
		totalTurns += s.Turns
	}
	meta := fmt.Sprintf("%d sessions · %d turns", len(m.manager.Sessions), totalTurns)
	right := m.styles.StatusDesc.Render(meta)

	// Left side: select-mode badge wins, then session-error fallback.
	var left string
	if m.selectMode {
		// Loud reverse-video badge so it's impossible to miss.
		badge := lipgloss.NewStyle().
			Foreground(colText).
			Background(colSecondary).
			Bold(true).
			Padding(0, 1).
			Render("✂ SELECT MODE")
		left = badge + m.styles.StatusDesc.Render(" ctrl+s to exit · drag with mouse · cmd+c copy")
	} else if cur := m.manager.Current(); cur != nil && cur.State == session.StateError && cur.LastErr != nil {
		left = m.styles.ResultError.Render("error: " + cur.LastErr.Error())
	}

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func prettyPath(p string) string {
	home := homedir()
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return filepath.Clean(p)
}
