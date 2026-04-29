package tui

import (
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// SettingsDialog is the central preferences modal. Today it owns just the
// theme picker, but the layout (single section header + a list of choices)
// is sized to grow into multiple sections later (model defaults, keymap,
// etc.) without redesigning the dialog.
//
// Theme rows are organized into three groups: an Auto entry at the top
// (follows the terminal background), then all dark themes, then all light
// themes. Section labels render between groups but are not selectable —
// the cursor is an index into the flat `entries` slice we build at open
// time, so navigation transparently skips over decorations.
type SettingsDialog struct {
	styles  Styles
	current string         // active theme id at open time, used to mark "(actual)"
	cursor  int            // index into entries
	entries []themeEntry   // flat list of selectable rows (id + Theme)
}

// themeEntry is a row in the picker. The Auto row has theme.ID == ""
// because there is no concrete Theme behind it — the swatches come from
// whichever default flavor Auto resolves to today.
type themeEntry struct {
	id    string // selection id passed back via Apply/Preview msgs (may be AutoThemeID)
	theme Theme  // concrete theme used for the swatches & subtitle
}

func NewSettingsDialog(currentThemeID string, s Styles) *SettingsDialog {
	entries := buildThemeEntries()
	cursor := 0
	for i, e := range entries {
		if e.id == currentThemeID {
			cursor = i
			break
		}
	}
	return &SettingsDialog{styles: s, current: currentThemeID, cursor: cursor, entries: entries}
}

// buildThemeEntries flattens the Themes catalog into the order shown in
// the picker: Auto, then every dark theme, then every light theme.
// Section labels are rendered separately by View().
func buildThemeEntries() []themeEntry {
	out := make([]themeEntry, 0, len(Themes)+1)
	// Auto first. We render its swatches using the dark default so users
	// see "Charple-ish" colors (the brand mark) without having to know
	// what their bg is right now.
	out = append(out, themeEntry{id: AutoThemeID, theme: ThemeByID(AutoDarkDefaultID)})
	for _, t := range Themes {
		if !t.Light {
			out = append(out, themeEntry{id: t.ID, theme: t})
		}
	}
	for _, t := range Themes {
		if t.Light {
			out = append(out, themeEntry{id: t.ID, theme: t})
		}
	}
	return out
}

func (d *SettingsDialog) Init() tea.Cmd { return nil }

func (d *SettingsDialog) Update(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch k.String() {
	case "esc", "ctrl+c", "q":
		// Revert the live preview back to whatever was active when the
		// dialog opened, then close.
		original := d.current
		return func() tea.Msg { return CancelSettingsMsg{OriginalID: original} }
	case "up", "k":
		if d.cursor > 0 {
			d.cursor--
			return d.preview()
		}
	case "down", "j":
		if d.cursor < len(d.entries)-1 {
			d.cursor++
			return d.preview()
		}
	case "enter", " ":
		choice := d.entries[d.cursor]
		return func() tea.Msg { return ApplyThemeMsg{ID: choice.id} }
	}
	return nil
}

// preview emits a non-persisting theme swap so the user sees the hovered
// theme applied live. The dialog stays open and `current` is unchanged
// (so esc still knows which theme to roll back to).
func (d *SettingsDialog) preview() tea.Cmd {
	id := d.entries[d.cursor].id
	return func() tea.Msg { return PreviewThemeMsg{ID: id} }
}

func (d *SettingsDialog) View(width, _ int) string {
	boxW := 56
	if boxW > width-4 {
		boxW = width - 4
	}
	if boxW < 44 {
		boxW = 44
	}
	innerW := boxW - 6

	title := HatchedTitle("Settings", innerW, colPrimary, colAccent, d.styles.DialogTitle)

	header := d.styles.HeaderTitle.Render("theme")
	rule := d.styles.HeaderSep.Render(strings.Repeat("─", innerW))

	var rows []string
	rows = append(rows, title, "", header, rule)

	// Walk entries in their flat order, inserting visual section labels
	// when the group changes. We don't change indices — cursor still
	// points into d.entries.
	prevGroup := ""
	for i, e := range d.entries {
		group := groupOf(e)
		if group != prevGroup {
			if i > 0 {
				rows = append(rows, "")
			}
			rows = append(rows, d.styles.Hint.Render(group))
			prevGroup = group
		}
		rows = append(rows, d.renderRow(e, i == d.cursor, e.id == d.current, innerW))
	}

	hint := d.styles.Hint.Render("↑↓ navegar · enter aplicar · esc cerrar")
	rows = append(rows, "", hint)

	return d.styles.DialogBox.Width(boxW).Render(strings.Join(rows, "\n"))
}

// groupOf returns the section heading for an entry. Auto is its own
// group so it visually sits apart from the explicit picks.
func groupOf(e themeEntry) string {
	switch {
	case e.id == AutoThemeID:
		return "auto"
	case e.theme.Light:
		return "light"
	default:
		return "dark"
	}
}

// renderRow draws one option: a cursor caret, the theme name (or Auto
// label), an "(actual)" marker on the active one, and a strip of color
// swatches so the user can preview the palette without applying.
func (d *SettingsDialog) renderRow(e themeEntry, focused, active bool, innerW int) string {
	caret := "  "
	nameStyle := d.styles.AssistantText
	if focused {
		caret = d.styles.UserPrompt.Render("▸ ")
		nameStyle = d.styles.AssistantText.Bold(true)
	}

	name := e.theme.Name
	if e.id == AutoThemeID {
		name = "Auto (sigue terminal)"
	}

	tag := ""
	if active {
		tag = " " + d.styles.Hint.Render("(actual)")
	}

	swatches := buildSwatches(e.theme.P)
	leftPart := caret + nameStyle.Render(name) + tag
	pad := innerW - lipgloss.Width(leftPart) - lipgloss.Width(swatches)
	if pad < 1 {
		pad = 1
	}
	return leftPart + strings.Repeat(" ", pad) + swatches
}

// buildSwatches paints a tiny "●" per palette accent so the user can see
// at a glance what they're picking. Order kept stable across themes.
func buildSwatches(p Palette) string {
	colors := []color.Color{p.Primary, p.Secondary, p.Tertiary, p.Accent, p.Warning, p.Danger}
	var sb strings.Builder
	for i, c := range colors {
		if i > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(lipgloss.NewStyle().Foreground(c).Render("●"))
	}
	return sb.String()
}
