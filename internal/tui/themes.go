package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Palette is the bag of colors the rest of the styles reference. We keep
// it as a flat struct (not nested) so swapping at runtime is a single
// assignment, and so package-level helpers (e.g. modal title gradients)
// can read named fields without threading a Styles instance.
//
// Each entry is built from a hex string at construction via the helper hex().
// We store color.Color (lipgloss v2's actual color type) rather than raw
// strings so styles can use them directly.
type Palette struct {
	Primary   color.Color
	Secondary color.Color
	Tertiary  color.Color
	Accent    color.Color
	Success   color.Color
	Warning   color.Color
	Danger    color.Color
	Muted     color.Color
	Text      color.Color
	Border    color.Color

	// Logo gradient endpoints. Most themes reuse Secondary/Primary, but
	// having dedicated fields lets a theme tune the brand mark separately.
	LogoTop color.Color
	LogoBot color.Color
	LogoVer color.Color
}

// hex is a thin alias for lipgloss.Color so theme literals stay readable.
func hex(s string) color.Color { return lipgloss.Color(s) }

// Theme bundles a palette with display metadata for the settings picker.
type Theme struct {
	ID    string  // stable key persisted in state.json
	Name  string  // shown in the picker
	Light bool    // hint: needs a light terminal background to look right
	P     Palette // the actual colors
}

// Themes is the curated list, in display order. The first entry is the
// default for fresh installs. Each theme aims for a different *mood* so
// users have real choice (not just hue rotations of the same palette):
//
//   - Charple Dark   — vibrant electric (default)
//   - Sunset Dark    — warm romantic
//   - Tokyo Night    — cool moody
//   - Synthwave      — neon party
//
// Constraints applied to every entry:
//   - Primary, Secondary, Tertiary, Accent are visually distinct hues so
//     the spinner / cursor / focus underline don't collapse together.
//   - Success/Warning/Danger keep semantic meaning regardless of the rest.
var Themes = []Theme{
	{
		ID:   "charple-dark",
		Name: "Charple Dark (default)",
		P: Palette{
			Primary:   hex("#6B50FF"), // Charple purple
			Secondary: hex("#FF60FF"), // Dolly magenta
			Tertiary:  hex("#68FFD6"), // Bok mint
			Accent:    hex("#BD93F9"),
			Success:   hex("#68FFD6"),
			Warning:   hex("#FFB86C"),
			Danger:    hex("#FF5555"),
			Muted:     hex("#858392"),
			Text:      hex("#DFDBDD"),
			Border:    hex("#44475A"),
			LogoTop:   hex("#FF60FF"),
			LogoBot:   hex("#6B50FF"),
			LogoVer:   hex("#68FFD6"),
		},
	},
	{
		ID:   "sunset-dark",
		Name: "Sunset Dark",
		P: Palette{
			Primary:   hex("#F97316"), // orange
			Secondary: hex("#EC4899"), // hot pink (cursor)
			Tertiary:  hex("#FBBF24"), // amber (focused prompt)
			Accent:    hex("#C084FC"), // lavender (tools)
			Success:   hex("#34D399"),
			Warning:   hex("#F59E0B"),
			Danger:    hex("#F87171"),
			Muted:     hex("#94A3B8"),
			Text:      hex("#F8FAFC"),
			Border:    hex("#334155"),
			LogoTop:   hex("#EC4899"),
			LogoBot:   hex("#F97316"),
			LogoVer:   hex("#FBBF24"),
		},
	},
	{
		// Premium / monochromatic-blue feel: every accent sits in the
		// blue→violet→cyan band so the UI reads as one cohesive palette.
		// Success/Warning/Danger are pulled toward the same value range
		// (muted teal, brushed gold, dusty rose) so they signal clearly
		// without breaking the cool-blue mood. Border is barely visible
		// against a dark terminal — the negative space carries the work
		// instead of competing rules.
		ID:   "tokyo-night",
		Name: "Tokyo Night",
		P: Palette{
			Primary:   hex("#7AA2F7"), // soft blue
			Secondary: hex("#BB9AF7"), // violet (cursor)
			Tertiary:  hex("#7DCFFF"), // cyan (focused prompt)
			Accent:    hex("#9D7CD8"), // deep purple (tools)
			Success:   hex("#73DACA"), // muted teal — same value as blue accents
			Warning:   hex("#C9A769"), // brushed gold — desaturated, not loud yellow
			Danger:    hex("#E06C75"), // dusty rose-red, calmer than #F7768E
			Muted:     hex("#545C7E"), // deep blue-gray, dims confidently
			Text:      hex("#C0CAF5"), // cool off-white with blue cast
			Border:    hex("#1F2335"), // near-black so panel rules recede
			LogoTop:   hex("#BB9AF7"),
			LogoBot:   hex("#7AA2F7"),
			LogoVer:   hex("#7DCFFF"),
		},
	},
	{
		ID:   "synthwave",
		Name: "Synthwave",
		P: Palette{
			Primary:   hex("#FF7EDB"), // neon pink
			Secondary: hex("#FF6EC7"), // hot magenta (cursor)
			Tertiary:  hex("#36F9F6"), // electric cyan
			Accent:    hex("#B084EB"), // electric lavender
			Success:   hex("#72F1B8"), // mint
			Warning:   hex("#FEDE5D"), // canary
			Danger:    hex("#FE4450"), // signal red
			Muted:     hex("#848BBD"),
			Text:      hex("#F8F8F2"),
			Border:    hex("#3B305B"),
			LogoTop:   hex("#FF7EDB"),
			LogoBot:   hex("#36F9F6"),
			LogoVer:   hex("#FEDE5D"),
		},
	},
}

// ThemeByID looks up a theme by its persisted ID. Returns the default
// (Themes[0]) when the id is unknown — keeps state.json forwards/backwards
// compatible if a theme gets renamed or removed.
func ThemeByID(id string) Theme {
	for _, t := range Themes {
		if t.ID == id {
			return t
		}
	}
	return Themes[0]
}
