package tui

import (
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/filepicker"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

var modelChoices = []string{"opus", "sonnet", "haiku"}
var effortChoices = []string{"low", "medium", "high", "xhigh", "max"}

type newSessionFocus int

const (
	focusPicker newSessionFocus = iota
	focusModel
	focusEffort
)

type NewSessionDialog struct {
	fp        filepicker.Model
	styles    Styles
	focus     newSessionFocus
	modelIdx  int
	effortIdx int
	err       string
}

func NewNewSessionDialog(defaultCwd, defaultModel, defaultEffort string, s Styles) *NewSessionDialog {
	fp := filepicker.New()
	fp.DirAllowed = true
	fp.FileAllowed = false
	fp.ShowHidden = false
	fp.ShowPermissions = false
	fp.ShowSize = false
	fp.AutoHeight = false
	fp.SetHeight(12)
	fp.Cursor = "›"
	if defaultCwd != "" {
		fp.CurrentDirectory = defaultCwd
	} else {
		fp.CurrentDirectory, _ = os.Getwd()
	}
	// Free up "enter" so it confirms the dialog (not descends into a dir).
	// Use right-arrow / l for descend, h / left / backspace for back.
	fp.KeyMap.Open = key.NewBinding(key.WithKeys("right", "l"))
	fp.KeyMap.Back = key.NewBinding(key.WithKeys("h", "backspace", "left"))
	fp.KeyMap.Select = key.NewBinding(key.WithKeys("never"))

	return &NewSessionDialog{
		fp:        fp,
		styles:    s,
		focus:     focusPicker,
		modelIdx:  indexOf(modelChoices, defaultModel),
		effortIdx: indexOf(effortChoices, defaultEffort),
	}
}

func indexOf(opts []string, v string) int {
	for i, o := range opts {
		if o == v {
			return i
		}
	}
	return 0
}

func (d *NewSessionDialog) Init() tea.Cmd {
	return d.fp.Init()
}

func (d *NewSessionDialog) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			return func() tea.Msg { return CloseDialogMsg{} }
		case "enter":
			return d.confirm()
		case "tab":
			d.focus = (d.focus + 1) % 3
			return nil
		case "shift+tab":
			d.focus = (d.focus + 2) % 3
			return nil
		}
		if d.focus == focusModel {
			switch k.String() {
			case "left", "h":
				if d.modelIdx > 0 {
					d.modelIdx--
				}
				return nil
			case "right", "l", " ":
				if d.modelIdx < len(modelChoices)-1 {
					d.modelIdx++
				}
				return nil
			}
		}
		if d.focus == focusEffort {
			switch k.String() {
			case "left", "h":
				if d.effortIdx > 0 {
					d.effortIdx--
				}
				return nil
			case "right", "l", " ":
				if d.effortIdx < len(effortChoices)-1 {
					d.effortIdx++
				}
				return nil
			}
		}
	}

	if d.focus == focusPicker {
		var cmd tea.Cmd
		d.fp, cmd = d.fp.Update(msg)
		return cmd
	}
	return nil
}

func (d *NewSessionDialog) confirm() tea.Cmd {
	cwd := strings.TrimSpace(d.fp.CurrentDirectory)
	if cwd == "" {
		d.err = "directorio vacío"
		return nil
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		d.err = err.Error()
		return nil
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		d.err = "no es un directorio: " + abs
		return nil
	}
	model := modelChoices[d.modelIdx]
	effort := effortChoices[d.effortIdx]
	return func() tea.Msg {
		return CreateSessionMsg{Cwd: abs, Model: model, Effort: effort}
	}
}

func (d *NewSessionDialog) View(width, height int) string {
	boxW := 72
	if boxW > width-4 {
		boxW = width - 4
	}
	if boxW < 40 {
		boxW = 40
	}
	fpH := height - 18
	if fpH > 14 {
		fpH = 14
	}
	if fpH < 6 {
		fpH = 6
	}
	d.fp.SetHeight(fpH)

	innerW := boxW - 6
	title := HatchedTitle("New Session", innerW, colPrimary, colAccent, d.styles.DialogTitle)
	pickerLabel := d.fieldLabel("directorio · "+d.fp.CurrentDirectory, d.focus == focusPicker)
	pickerView := d.fp.View()
	pickerHints := d.styles.Hint.Render("↑↓ navegar · → descender · ← atrás")

	modelLabel := d.fieldLabel("modelo", d.focus == focusModel)
	modelRow := d.radioRow(modelChoices, d.modelIdx, d.focus == focusModel)

	effortLabel := d.fieldLabel("effort", d.focus == focusEffort)
	effortRow := d.radioRow(effortChoices, d.effortIdx, d.focus == focusEffort)

	hints := d.styles.StatusKey.Render("enter") + d.styles.Hint.Render(" crear  ") +
		d.styles.StatusKey.Render("tab") + d.styles.Hint.Render(" siguiente campo  ") +
		d.styles.StatusKey.Render("←/→") + d.styles.Hint.Render(" cambiar opción  ") +
		d.styles.StatusKey.Render("esc") + d.styles.Hint.Render(" cancelar")

	lines := []string{
		title, "",
		pickerLabel,
		pickerView,
		pickerHints,
		"",
		modelLabel,
		modelRow,
		"",
		effortLabel,
		effortRow,
	}
	if d.err != "" {
		lines = append(lines, "", d.styles.ResultError.Render("✗ "+d.err))
	}
	lines = append(lines, "", hints)

	return d.styles.DialogBox.Width(boxW).Render(strings.Join(lines, "\n"))
}

func (d *NewSessionDialog) fieldLabel(text string, focused bool) string {
	if focused {
		return d.styles.UserPrompt.Render("▸ ") + d.styles.HeaderTitle.Render(text)
	}
	return "  " + d.styles.HeaderDim.Render(text)
}

func (d *NewSessionDialog) radioRow(opts []string, sel int, focused bool) string {
	var parts []string
	for i, o := range opts {
		mark := "○"
		st := d.styles.Hint
		if i == sel {
			mark = "●"
			st = d.styles.StatusIdle
			if focused {
				st = d.styles.UserPrompt
			}
		}
		parts = append(parts, st.Render(mark+" "+o))
	}
	return "  " + strings.Join(parts, "  ")
}
