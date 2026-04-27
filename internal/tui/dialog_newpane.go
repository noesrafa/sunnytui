package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/noesrafa/sunnytui/internal/favs"
)

func defaultShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/bash"
}

type NewPaneDialog struct {
	nameIn    textinput.Model
	cmdIn     textinput.Model
	cwdIn     textinput.Model
	focus     int // 0 name, 1 cmd, 2 cwd
	styles    Styles
	err       string
	saved     string // ephemeral "saved as fav" message
	favorites []favs.Favorite
}

func NewNewPaneDialog(defaultCwd string, s Styles) *NewPaneDialog {
	mk := func(placeholder, value string) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = placeholder
		ti.Prompt = "› "
		ti.SetValue(value)
		ti.SetWidth(60)
		return ti
	}
	defaultName := "shell"
	if defaultCwd != "" {
		defaultName = filepath.Base(defaultCwd)
	}
	favs, _ := favs.Load()
	d := &NewPaneDialog{
		nameIn:    mk(defaultName, defaultName),
		cmdIn:     mk("(blank → "+defaultShell()+")", ""),
		cwdIn:     mk(defaultCwd, defaultCwd),
		styles:    s,
		favorites: favs,
	}
	d.nameIn.Focus()
	return d
}

func (d *NewPaneDialog) Init() tea.Cmd { return textinput.Blink }

func (d *NewPaneDialog) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyMsg); ok {
		s := k.String()
		// Number keys 1-9 → instantly fill from favorites at that index.
		if len(s) == 1 && s >= "1" && s <= "9" {
			idx := int(s[0] - '1')
			if idx < len(d.favorites) {
				f := d.favorites[idx]
				d.nameIn.SetValue(f.Name)
				d.cmdIn.SetValue(f.Command)
				if f.Cwd != "" {
					d.cwdIn.SetValue(f.Cwd)
				}
				d.saved = ""
				return nil
			}
		}
		switch s {
		case "esc":
			return func() tea.Msg { return CloseDialogMsg{} }
		case "tab":
			d.advance(1)
			return nil
		case "shift+tab":
			d.advance(-1)
			return nil
		case "ctrl+f":
			// Save current values as a new favorite.
			name := strings.TrimSpace(d.nameIn.Value())
			cmd := strings.TrimSpace(d.cmdIn.Value())
			if name == "" || cmd == "" {
				d.err = "fill name+command before saving fav"
				return nil
			}
			updated, err := favs.Add(name, cmd, strings.TrimSpace(d.cwdIn.Value()))
			if err != nil {
				d.err = err.Error()
				return nil
			}
			d.favorites = updated
			d.saved = "★ saved as " + name
			d.err = ""
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

func (d *NewPaneDialog) advance(by int) {
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

func (d *NewPaneDialog) submit() tea.Cmd {
	name := strings.TrimSpace(d.nameIn.Value())
	command := strings.TrimSpace(d.cmdIn.Value())
	cwd := strings.TrimSpace(d.cwdIn.Value())
	if name == "" {
		name = "shell"
	}
	if command == "" {
		command = defaultShell()
	}
	if cwd != "" {
		info, err := os.Stat(cwd)
		if err != nil || !info.IsDir() {
			d.err = "cwd no es directorio: " + cwd
			return nil
		}
	}
	return func() tea.Msg {
		return CreatePaneMsg{Name: name, Command: command, Cwd: cwd}
	}
}

func (d *NewPaneDialog) View(width, height int) string {
	boxW := 76
	if boxW > width-4 {
		boxW = width - 4
	}
	if boxW < 40 {
		boxW = 40
	}
	innerW := boxW - 6
	title := HatchedTitle("New Terminal", innerW, colPrimary, colSecondary, d.styles.DialogTitle)

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

	// Favorites quick-pick row: "1 claude · 2 yolo · 3 shell …"
	if len(d.favorites) > 0 {
		var picks []string
		max := len(d.favorites)
		if max > 9 {
			max = 9
		}
		for i := 0; i < max; i++ {
			f := d.favorites[i]
			picks = append(picks, d.styles.StatusKey.Render(fmt.Sprintf("%d", i+1))+
				" "+d.styles.AssistantText.Render(f.Name))
		}
		lines = append(lines,
			d.styles.HeaderDim.Render("favorites"),
			"  "+strings.Join(picks, d.styles.Hint.Render(" · ")),
			"")
	}

	lines = append(lines, field("name", d.focus == 0, d.nameIn.View())...)
	lines = append(lines, "")
	lines = append(lines, field("command", d.focus == 1, d.cmdIn.View())...)
	lines = append(lines, "")
	lines = append(lines, field("cwd", d.focus == 2, d.cwdIn.View())...)

	if d.saved != "" {
		lines = append(lines, "", d.styles.StatusIdle.Render(d.saved))
	}
	if d.err != "" {
		lines = append(lines, "", d.styles.ResultError.Render("✗ "+d.err))
	}
	hints := strings.Join([]string{
		d.styles.StatusKey.Render("enter") + d.styles.Hint.Render(" spawn"),
		d.styles.StatusKey.Render("1-9") + d.styles.Hint.Render(" pick fav"),
		d.styles.StatusKey.Render("ctrl+f") + d.styles.Hint.Render(" save fav"),
		d.styles.StatusKey.Render("tab") + d.styles.Hint.Render(" next"),
		d.styles.StatusKey.Render("esc") + d.styles.Hint.Render(" cancel"),
	}, d.styles.Hint.Render(" · "))
	lines = append(lines, "", hints)
	return d.styles.DialogBox.Width(boxW).Render(strings.Join(lines, "\n"))
}
