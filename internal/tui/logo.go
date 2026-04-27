package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

const Version = "0.2.0"

// Block-art SUNNY: 5 letters × 4 cols × 5 rows + 1-col gaps = 24 cols wide.
var sunnyBlock = []string{
	"████ █  █ █  █ █  █ █  █",
	"█    █  █ ██ █ ██ █  ██ ",
	"████ █  █ █ ██ █ ██   █ ",
	"   █ █  █ █  █ █  █   █ ",
	"████ ████ █  █ █  █   █ ",
}

const logoBlockW = 24

// renderLogo paints a Crush-style logo in a single purple tone:
//   - top hatching (purple)
//   - "sunnytui™ … v0.1.0" brand row (muted left, cyan right)
//   - SUNNY block letters (purple)
//   - bottom hatching (purple)
func renderLogo(width int, s Styles) string {
	if width < logoBlockW {
		width = logoBlockW
	}
	hatch := s.LogoBot.Render(strings.Repeat("╱", width))

	pad := (width - logoBlockW) / 2
	if pad < 0 {
		pad = 0
	}
	padStr := strings.Repeat(" ", pad)

	brand := s.LogoBrand.Render("sunnytui™")
	ver := s.LogoVer.Render("v" + Version)
	leftW := lipgloss.Width(brand)
	rightW := lipgloss.Width(ver)
	gap := width - leftW - rightW
	if gap < 1 {
		gap = 1
	}
	brandRow := brand + strings.Repeat(" ", gap) + ver

	var letters []string
	for _, row := range sunnyBlock {
		letters = append(letters, padStr+s.LogoBot.Render(row))
	}

	lines := []string{hatch, brandRow}
	lines = append(lines, letters...)
	lines = append(lines, hatch)
	return strings.Join(lines, "\n")
}
