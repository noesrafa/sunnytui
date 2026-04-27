package tui

import (
	"os"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// RunEditDialog is the "new run" form (no edit-existing yet — delete + new).
type RunEditDialog struct {
	form   formInputs
	styles Styles
	err    string
}

const (
	runFieldName = iota
	runFieldCommand
	runFieldCwd
)

func NewRunEditDialog(defaultCwd string, s Styles) *RunEditDialog {
	form := newFormInputs([]inputSpec{
		{Placeholder: "e.g. web"},
		{Placeholder: "e.g. bun run dev"},
		{Placeholder: defaultCwd},
	})
	return &RunEditDialog{form: form, styles: s}
}

func (d *RunEditDialog) Init() tea.Cmd { return textinput.Blink }

func (d *RunEditDialog) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			return func() tea.Msg { return CloseDialogMsg{} }
		case "tab":
			d.form.advance(1)
			return nil
		case "shift+tab":
			d.form.advance(-1)
			return nil
		case "enter":
			return d.submit()
		}
	}
	return d.form.updateActive(msg)
}

func (d *RunEditDialog) submit() tea.Cmd {
	name := strings.TrimSpace(d.form.value(runFieldName))
	cmd := strings.TrimSpace(d.form.value(runFieldCommand))
	cwd := strings.TrimSpace(d.form.value(runFieldCwd))
	if name == "" {
		d.err = "name requerido"
		return nil
	}
	if cmd == "" {
		d.err = "comando requerido"
		return nil
	}
	if cwd != "" {
		info, err := os.Stat(cwd)
		if err != nil || !info.IsDir() {
			d.err = "cwd no es directorio: " + cwd
			return nil
		}
	}
	return func() tea.Msg {
		return CreateRunMsg{Name: name, Command: cmd, Cwd: cwd}
	}
}

func (d *RunEditDialog) View(width, height int) string {
	boxW := 70
	if boxW > width-4 {
		boxW = width - 4
	}
	if boxW < 40 {
		boxW = 40
	}
	innerW := boxW - 6

	d.form.setWidth(innerW - 4)

	title := HatchedTitle("New Run", innerW, colPrimary, colAccent, d.styles.DialogTitle)

	lines := []string{title, ""}
	lines = append(lines, formFieldView("name", d.form.focus == runFieldName, d.form.view(runFieldName), d.styles)...)
	lines = append(lines, "")
	lines = append(lines, formFieldView("command", d.form.focus == runFieldCommand, d.form.view(runFieldCommand), d.styles)...)
	lines = append(lines, "")
	lines = append(lines, formFieldView("cwd (optional)", d.form.focus == runFieldCwd, d.form.view(runFieldCwd), d.styles)...)
	if d.err != "" {
		lines = append(lines, "", d.styles.ResultError.Render("✗ "+d.err))
	}
	hints := strings.Join([]string{
		d.styles.StatusKey.Render("enter") + d.styles.Hint.Render(" save"),
		d.styles.StatusKey.Render("tab") + d.styles.Hint.Render(" next field"),
		d.styles.StatusKey.Render("esc") + d.styles.Hint.Render(" cancel"),
	}, d.styles.Hint.Render(" · "))
	lines = append(lines, "", hints)
	return d.styles.DialogBox.Width(boxW).Render(strings.Join(lines, "\n"))
}
