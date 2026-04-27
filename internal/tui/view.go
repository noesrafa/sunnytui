package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/noesrafa/sunnytui/internal/session"
)

func (m Model) View() tea.View {
	v := tea.NewView("starting…")
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	if !m.ready {
		return v
	}
	base := m.renderBase()
	if m.overlay.HasOpen() {
		v.SetContent(m.composeWithModal(base))
	} else {
		v.SetContent(base)
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
	sidebar := renderSidebar(m.manager, bodyH, m.styles)
	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, main)
}

func (m Model) renderMain(height int) string {
	cur := m.manager.Current()
	transcript := m.viewport.View()
	input := m.renderInput(cur)
	hint := m.renderInputHint()

	mainW := m.width - sidebarWidth - 1
	if mainW < 20 {
		mainW = 20
	}
	body := lipgloss.JoinVertical(lipgloss.Left, transcript, input, hint)
	return lipgloss.NewStyle().Width(mainW).Height(height).Render(body)
}

func (m Model) renderInput(cur *session.Session) string {
	style := m.styles.Input
	if cur != nil && cur.State == session.StateIdle {
		style = m.styles.InputFocused
	}
	mainW := m.width - sidebarWidth - 1
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

	// Show error message on the left if the active session has one.
	var left string
	if cur := m.manager.Current(); cur != nil && cur.State == session.StateError && cur.LastErr != nil {
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
