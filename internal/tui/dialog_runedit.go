package tui

import (
	"os"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// RunEditDialog is the "new run" form (no edit-existing yet — delete + new).
type RunEditDialog struct {
	nameIn  textinput.Model
	cmdIn   textinput.Model
	cwdIn   textinput.Model
	focus   int // 0 name, 1 command, 2 cwd
	styles  Styles
	err     string
}

func NewRunEditDialog(defaultCwd string, s Styles) *RunEditDialog {
	mk := func(placeholder, value string) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = placeholder
		ti.Prompt = "› "
		ti.SetValue(value)
		ti.SetWidth(60)
		return ti
	}
	d := &RunEditDialog{
		nameIn: mk("e.g. web", ""),
		cmdIn:  mk("e.g. bun run dev", ""),
		cwdIn:  mk(defaultCwd, ""),
		styles: s,
	}
	d.nameIn.Focus()
	return d
}

func (d *RunEditDialog) Init() tea.Cmd { return textinput.Blink }

func (d *RunEditDialog) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			return func() tea.Msg { return CloseDialogMsg{} }
		case "tab":
			d.advance(1)
			return nil
		case "shift+tab":
			d.advance(-1)
			return nil
		case "enter":
			return d.submit()
		}
	}
	var cmd tea.Cmd
	switch d.focus {
	case 0:
		d.nameIn, cmd = d.nameIn.Update(msg)
	case 1:
		d.cmdIn, cmd = d.cmdIn.Update(msg)
	case 2:
		d.cwdIn, cmd = d.cwdIn.Update(msg)
	}
	return cmd
}

func (d *RunEditDialog) advance(by int) {
	d.nameIn.Blur()
	d.cmdIn.Blur()
	d.cwdIn.Blur()
	d.focus = (d.focus + by + 3) % 3
	switch d.focus {
	case 0:
		d.nameIn.Focus()
	case 1:
		d.cmdIn.Focus()
	case 2:
		d.cwdIn.Focus()
	}
}

func (d *RunEditDialog) submit() tea.Cmd {
	name := strings.TrimSpace(d.nameIn.Value())
	cmd := strings.TrimSpace(d.cmdIn.Value())
	cwd := strings.TrimSpace(d.cwdIn.Value())
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

	title := HatchedTitle("New Run", innerW, colPrimary, colAccent, d.styles.DialogTitle)

	d.nameIn.SetWidth(innerW - 4)
	d.cmdIn.SetWidth(innerW - 4)
	d.cwdIn.SetWidth(innerW - 4)

	field := func(label string, focused bool, view string) []string {
		head := d.styles.HeaderDim.Render(label)
		if focused {
			head = d.styles.UserPrompt.Render("▸ ") + d.styles.HeaderTitle.Render(label)
		} else {
			head = "  " + head
		}
		return []string{head, "  " + view}
	}

	lines := []string{title, ""}
	lines = append(lines, field("name", d.focus == 0, d.nameIn.View())...)
	lines = append(lines, "")
	lines = append(lines, field("command", d.focus == 1, d.cmdIn.View())...)
	lines = append(lines, "")
	lines = append(lines, field("cwd (optional)", d.focus == 2, d.cwdIn.View())...)
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
