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
type SettingsDialog struct {
	styles  Styles
	current string // active theme id at open time, used to mark "(actual)"
	cursor  int    // index into Themes
}

func NewSettingsDialog(currentThemeID string, s Styles) *SettingsDialog {
	cursor := 0
	for i, t := range Themes {
		if t.ID == currentThemeID {
			cursor = i
			break
		}
	}
	return &SettingsDialog{styles: s, current: currentThemeID, cursor: cursor}
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
		if d.cursor < len(Themes)-1 {
			d.cursor++
			return d.preview()
		}
	case "enter", " ":
		choice := Themes[d.cursor]
		return func() tea.Msg { return ApplyThemeMsg{ID: choice.ID} }
	}
	return nil
}

// preview emits a non-persisting theme swap so the user sees the hovered
// theme applied live. The dialog stays open and `current` is unchanged
// (so esc still knows which theme to roll back to).
func (d *SettingsDialog) preview() tea.Cmd {
	id := Themes[d.cursor].ID
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
	for i, t := range Themes {
		rows = append(rows, d.renderThemeRow(t, i == d.cursor, t.ID == d.current, innerW))
	}

	hint := d.styles.Hint.Render("↑↓ navegar · enter aplicar · esc cerrar")
	rows = append(rows, "", hint)

	return d.styles.DialogBox.Width(boxW).Render(strings.Join(rows, "\n"))
}

// renderThemeRow draws one option: a cursor caret, the theme name, an
// "(actual)" marker on the active one, and a strip of color swatches so
// the user can preview the palette without applying.
func (d *SettingsDialog) renderThemeRow(t Theme, focused, active bool, innerW int) string {
	caret := "  "
	nameStyle := d.styles.AssistantText
	if focused {
		caret = d.styles.UserPrompt.Render("▸ ")
		nameStyle = d.styles.AssistantText.Bold(true)
	}
	tag := ""
	if active {
		tag = " " + d.styles.Hint.Render("(actual)")
	}

	swatches := buildSwatches(t.P)
	leftPart := caret + nameStyle.Render(t.Name) + tag
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
