package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/rivo/uniseg"
)

// HatchedTitle renders "title ╱╱╱╱…" filling `width` with a diagonal hatch in
// a horizontal gradient between fromCol and toCol. This mirrors Crush's modal
// title bar (see /tmp/charm-crush/internal/ui/common/elements.go).
func HatchedTitle(title string, width int, fromCol, toCol color.Color, titleStyle lipgloss.Style) string {
	titleRendered := titleStyle.Render(title)
	titleW := lipgloss.Width(titleRendered)
	remaining := width - titleW - 1
	if remaining <= 0 {
		return titleRendered
	}
	hatch := strings.Repeat("╱", remaining)
	hatch = applyForegroundGradient(hatch, fromCol, toCol)
	return titleRendered + " " + hatch
}

// applyForegroundGradient colors each grapheme cluster of s with a position
// in the gradient between from and to. Adapted from Crush's grad.go using
// lipgloss.Blend1D, which exists in lipgloss v2.
func applyForegroundGradient(s string, from, to color.Color) string {
	if s == "" {
		return ""
	}
	var clusters []string
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		clusters = append(clusters, string(g.Runes()))
	}
	n := len(clusters)
	if n == 1 {
		return lipgloss.NewStyle().Foreground(from).Render(clusters[0])
	}
	ramp := lipgloss.Blend1D(n, from, to)
	var b strings.Builder
	for i, c := range clusters {
		b.WriteString(lipgloss.NewStyle().Foreground(ramp[i]).Render(c))
	}
	return b.String()
}
