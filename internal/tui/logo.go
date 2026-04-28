package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

const Version = "0.8.0"

// Block-art SUNNY: 5 letters × 4 cols × 5 rows + 1-col gaps = 24 cols wide.
var sunnyBlock = []string{
	"████ █  █ █  █ █  █ █  █",
	"█    █  █ ██ █ ██ █  ██ ",
	"████ █  █ █ ██ █ ██   █ ",
	"   █ █  █ █  █ █  █   █ ",
	"████ ████ █  █ █  █   █ ",
}

const logoBlockW = 24

// Logo gradient ramp cache. The ramp itself is identical every frame —
// only the per-column phase shifts — so recomputing Blend1D on each tick
// is pure waste. Cache by (top, bot) color identity; when the active
// palette changes (settings dialog), the next render rebuilds.
var (
	cachedLogoRamp    []color.Color
	cachedLogoRampTop color.Color
	cachedLogoRampBot color.Color
)

func logoColorRamp() []color.Color {
	if cachedLogoRamp == nil || cachedLogoRampTop != colLogoTop || cachedLogoRampBot != colLogoBot {
		cachedLogoRamp = lipgloss.Blend1D(logoBlockW, colLogoTop, colLogoBot)
		cachedLogoRampTop = colLogoTop
		cachedLogoRampBot = colLogoBot
	}
	return cachedLogoRamp
}

// renderLogo paints the brand mark with an animated gradient sweep across
// the SUNNY letters. `frame` is a monotonically-increasing counter
// (driven by logoTick in model.go); each step shifts the gradient phase
// by one column so the colors flow continuously left → right and bounce
// back. When frame is 0 the logo renders as a static gradient (the same
// as before the animation was added), so it's safe to call from non-tea
// contexts too.
//
// Layout (unchanged):
//
//	╱╱╱╱╱╱╱╱…   ← top hatching
//	sunnytui™ … v0.x.y
//	█████ █ █ ███ █ █ █ █   ← SUNNY block letters
//	╱╱╱╱╱╱╱╱…   ← bottom hatching
func renderLogo(width int, s Styles, frame int) string {
	if width < logoBlockW {
		width = logoBlockW
	}
	hatchTop := s.LogoTop.Render(strings.Repeat("╱", width))
	hatchBot := s.LogoBot.Render(strings.Repeat("╱", width))

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

	// Build the per-column color ramp by sliding the LogoTop→LogoBot
	// gradient across a palindromic 2× space. The bounce gives the
	// animation a calm, breathing feel — straight wraparound felt jumpy.
	// We sample one ramp for the whole letter block (all 5 rows share
	// columns), so the same column lights up the same color top-to-
	// bottom — reads as a coherent vertical band sliding across.
	ramp := logoColorRamp()
	cols := make([]color.Color, logoBlockW)
	span := 2 * logoBlockW
	for x := 0; x < logoBlockW; x++ {
		pos := (x + frame) % span
		if pos < 0 {
			pos += span
		}
		if pos >= logoBlockW {
			pos = span - 1 - pos
		}
		cols[x] = ramp[pos]
	}

	var letters []string
	for _, row := range sunnyBlock {
		letters = append(letters, padStr+colorizeLetterRow(row, cols))
	}

	lines := []string{hatchTop, brandRow}
	lines = append(lines, letters...)
	lines = append(lines, hatchBot)
	return strings.Join(lines, "\n")
}

// colorizeLetterRow paints each filled cell ("█") with cols[x] and leaves
// spaces untouched. Building the styles per-cell would explode tokens for
// no visual gain, so we batch consecutive cells of the same color into
// one Render call.
func colorizeLetterRow(row string, cols []color.Color) string {
	var b strings.Builder
	runes := []rune(row)
	i := 0
	for i < len(runes) {
		// Skip blank runs verbatim.
		if runes[i] == ' ' {
			j := i
			for j < len(runes) && runes[j] == ' ' {
				j++
			}
			b.WriteString(string(runes[i:j]))
			i = j
			continue
		}
		// Group consecutive filled cells that share the same column color
		// (rare: identical adjacent ramp entries from rounding).
		j := i
		c := cols[i]
		for j < len(runes) && runes[j] != ' ' && j-i < 16 && colorsEqual(cols[j], c) {
			j++
		}
		b.WriteString(lipgloss.NewStyle().Foreground(c).Render(string(runes[i:j])))
		i = j
	}
	return b.String()
}

// colorsEqual compares two color.Color values via their RGBA tuples.
// Cheap enough for the per-frame logo render and avoids importing reflect.
func colorsEqual(a, b color.Color) bool {
	if a == b {
		return true
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}
