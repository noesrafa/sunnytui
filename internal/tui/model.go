package tui

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"

	"github.com/noesrafa/sunnytui/internal/session"
)

type Model struct {
	width  int
	height int
	ready  bool

	ctx    context.Context
	styles Styles
	keymap KeyMap
	logger *log.Logger

	manager       *session.Manager
	overlay       *Overlay
	initialCwd    string
	defaultModel  string
	defaultEffort string
	skipPerms     bool
	lastErr       error
	lastCtrlC     time.Time

	viewport viewport.Model
	textarea textarea.Model
	spinner  spinner.Model

	md      *glamour.TermRenderer
	mdW     int
	mdCache map[string]string
}

type Options struct {
	Logger                   *log.Logger
	DefaultModel             string
	DefaultEffort            string
	DangerousSkipPermissions bool
}

const (
	headerHeight   = 1
	statusHeight   = 1
	textareaMinH   = 3
	textareaMaxH   = 12
	ctrlCDoubleWin = 1500 * time.Millisecond
)

func NewModel(ctx context.Context, mgr *session.Manager, initialCwd string, opts Options) Model {
	st := DefaultStyles()
	km := DefaultKeyMap()

	ta := textarea.New()
	ta.Placeholder = "escribe tu mensaje y enter para enviar (shift+enter o ctrl+j: nueva línea · \\+enter también)"
	ta.Prompt = "› "
	ta.CharLimit = -1
	ta.ShowLineNumbers = false
	ta.SetVirtualCursor(false)
	// Crush pattern: let bubbles handle dynamic height between min/max.
	ta.DynamicHeight = true
	ta.MinHeight = textareaMinH
	ta.MaxHeight = textareaMaxH
	ta.KeyMap.WordForward = key.NewBinding(key.WithKeys("alt+right", "alt+f", "ctrl+right"))
	ta.KeyMap.WordBackward = key.NewBinding(key.WithKeys("alt+left", "alt+b", "ctrl+left"))
	ta.KeyMap.DeleteWordForward = key.NewBinding(key.WithKeys("alt+d", "ctrl+delete"))
	ta.KeyMap.DeleteWordBackward = key.NewBinding(key.WithKeys("alt+backspace", "ctrl+backspace"))
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j", "alt+enter", "shift+enter"))
	ta.Focus()

	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	sp.Style = lipgloss.NewStyle().Foreground(colWarning)

	vp := viewport.New()
	vp.SetWidth(80)
	vp.SetHeight(20)

	defModel := opts.DefaultModel
	if defModel == "" {
		defModel = "opus"
	}
	defEffort := opts.DefaultEffort
	if defEffort == "" {
		defEffort = "max"
	}

	return Model{
		ctx:           ctx,
		styles:        st,
		keymap:        km,
		logger:        opts.Logger,
		manager:       mgr,
		overlay:       &Overlay{},
		viewport:      vp,
		textarea:      ta,
		spinner:       sp,
		initialCwd:    initialCwd,
		defaultModel:  defModel,
		defaultEffort: defEffort,
		skipPerms:     opts.DangerousSkipPermissions,
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{textarea.Blink}
	if m.anyThinking() {
		cmds = append(cmds, m.spinner.Tick)
	}
	for _, s := range m.manager.Sessions {
		cmds = append(cmds, waitForSession(s))
	}
	return tea.Batch(cmds...)
}

func waitForSession(sess *session.Session) tea.Cmd {
	id := sess.ID
	events := sess.Stream.Events()
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return sessionClosedMsg{SessionID: id}
		}
		return sessionEventMsg{SessionID: id, Event: ev}
	}
}

func (m Model) anyThinking() bool {
	for _, s := range m.manager.Sessions {
		if s.State == session.StateThinking {
			return true
		}
	}
	return false
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	switch v := msg.(type) {
	case CloseDialogMsg:
		m.overlay.CloseTop()
		return m, nil
	case ConfirmQuitMsg:
		return m, tea.Quit
	case CreateSessionMsg:
		m.overlay.CloseTop()
		if v.Model != "" {
			m.defaultModel = v.Model
		}
		if v.Effort != "" {
			m.defaultEffort = v.Effort
		}
		// Save current draft before switching to the new session.
		if cur := m.manager.Current(); cur != nil {
			cur.Draft = m.textarea.Value()
		}
		s, err := session.New(m.ctx, v.Cwd, session.Options{
			Logger:                   m.logger,
			Model:                    v.Model,
			Effort:                   v.Effort,
			DangerousSkipPermissions: m.skipPerms,
		})
		if err != nil {
			m.lastErr = err
			if m.logger != nil {
				m.logger.Error("create session failed", "err", err, "cwd", v.Cwd)
			}
			return m, nil
		}
		m.manager.Add(s)
		m.textarea.Reset() // new session starts with empty draft
		m.layout()
		m.refreshViewport()
		return m, waitForSession(s)
	case RenameSessionMsg:
		m.overlay.CloseTop()
		if cur := m.manager.Current(); cur != nil {
			cur.Title = v.NewTitle
			if m.logger != nil {
				m.logger.Info("session renamed", "session", cur.ID, "title", v.NewTitle)
			}
		}
		return m, nil
	}

	// Only wheel events route to the viewport. Click/motion are dropped to
	// avoid surprising selection behavior on top of the chat.
	if mm, isWheel := msg.(tea.MouseWheelMsg); isWheel {
		if !m.overlay.HasOpen() {
			m.viewport, cmd = m.viewport.Update(mm)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}
	if _, isMouse := msg.(tea.MouseMsg); isMouse {
		return m, nil
	}

	if m.overlay.HasOpen() {
		if _, isKey := msg.(tea.KeyMsg); isKey {
			return m, m.overlay.UpdateTop(msg)
		}
		cmds = append(cmds, m.overlay.UpdateTop(msg))
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.md = nil
		m.mdCache = nil
		m.refreshViewport()
		m.ready = true

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keymap.Quit):
			return m, m.openQuitDialog()
		case key.Matches(msg, m.keymap.ClearOrCancel):
			if !m.lastCtrlC.IsZero() && time.Since(m.lastCtrlC) < ctrlCDoubleWin {
				// Second press → cancel current turn.
				m.lastCtrlC = time.Time{}
				cur := m.manager.Current()
				if cur != nil && cur.State == session.StateThinking {
					if err := cur.Cancel(); err != nil {
						cur.LastErr = err
					}
				}
			} else {
				// First press → clear textarea, start the double-press timer.
				m.textarea.Reset()
				if cur := m.manager.Current(); cur != nil {
					cur.Draft = ""
				}
				m.lastCtrlC = time.Now()
				m.layout()
			}
			return m, nil
		case key.Matches(msg, m.keymap.NewSession):
			d := NewNewSessionDialog(m.initialCwd, m.defaultModel, m.defaultEffort, m.styles)
			return m, m.overlay.Open(d)
		case key.Matches(msg, m.keymap.Rename):
			cur := m.manager.Current()
			if cur != nil {
				d := NewRenameDialog(cur.Title, m.styles)
				return m, m.overlay.Open(d)
			}
			return m, nil
		case key.Matches(msg, m.keymap.NextSession):
			m.switchSession(func() { m.manager.Next() })
			return m, nil
		case key.Matches(msg, m.keymap.PrevSession):
			m.switchSession(func() { m.manager.Prev() })
			return m, nil
		case key.Matches(msg, m.keymap.CloseSession):
			cur := m.manager.Current()
			if cur != nil {
				m.manager.Close(cur.ID)
				// Load the new current's draft (if any).
				if next := m.manager.Current(); next != nil {
					m.textarea.SetValue(next.Draft)
					m.textarea.CursorEnd()
				} else {
					m.textarea.Reset()
				}
				m.layout()
				m.refreshViewport()
			}
			return m, nil
		case key.Matches(msg, m.keymap.Send):
			cur := m.manager.Current()
			if cur == nil || cur.State != session.StateIdle {
				return m, nil
			}
			value := m.textarea.Value()
			// Crush pattern: trailing backslash escapes Enter to a newline
			// (so users can compose multiline messages without Shift+Enter).
			if before, ok := strings.CutSuffix(value, "\\"); ok {
				m.textarea.SetValue(before + "\n")
				m.textarea.CursorEnd()
				m.layout()
				return m, nil
			}
			text := strings.TrimSpace(value)
			if text == "" {
				return m, nil
			}
			m.textarea.Reset()
			cur.Draft = ""
			if err := cur.Send(text); err != nil {
				cur.LastErr = err
			}
			m.layout()
			m.refreshViewport()
			m.viewport.GotoBottom()
			return m, m.spinner.Tick
		}

	case sessionEventMsg:
		sess := m.manager.ByID(msg.SessionID)
		if sess != nil {
			sess.HandleEvent(msg.Event)
			if cur := m.manager.Current(); cur != nil && cur.ID == sess.ID {
				m.refreshViewport()
				m.viewport.GotoBottom()
			}
			cmds = append(cmds, waitForSession(sess))
		}

	case sessionClosedMsg:
		m.manager.Close(msg.SessionID)
		m.refreshViewport()

	case spinner.TickMsg:
		if !m.anyThinking() {
			return m, tea.Batch(cmds...)
		}
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
		if cur := m.manager.Current(); cur != nil && cur.State == session.StateThinking {
			m.refreshViewport()
		}
	}

	if !m.overlay.HasOpen() {
		prevValue := m.textarea.Value()
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
		if m.textarea.Value() != prevValue {
			m.layout() // dynamic textarea height
		}
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// switchSession saves current textarea as draft, runs the navigation func,
// then restores the new current session's draft into the textarea.
func (m *Model) switchSession(nav func()) {
	if cur := m.manager.Current(); cur != nil {
		cur.Draft = m.textarea.Value()
	}
	nav()
	if next := m.manager.Current(); next != nil {
		m.textarea.SetValue(next.Draft)
		m.textarea.CursorEnd()
	}
	m.layout()
	m.refreshViewport()
	m.viewport.GotoBottom()
}

func (m Model) openQuitDialog() tea.Cmd {
	anyThinking := false
	for _, s := range m.manager.Sessions {
		if s.State == session.StateThinking {
			anyThinking = true
			break
		}
	}
	d := NewQuitDialog(m.styles, len(m.manager.Sessions), anyThinking)
	return m.overlay.Open(d)
}

func (m *Model) layout() {
	mainW := m.width - sidebarWidth - 1
	if mainW < 20 {
		mainW = 20
	}

	// textarea grows itself via DynamicHeight; we just read what it picked.
	taH := m.textarea.Height()
	if taH < textareaMinH {
		taH = textareaMinH
	}

	bodyH := m.height - headerHeight - statusHeight
	if bodyH < 6 {
		bodyH = 6
	}
	inputBoxH := taH + 2 // border
	hintH := 1           // hint row below input
	vpH := bodyH - inputBoxH - hintH
	if vpH < 3 {
		vpH = 3
	}
	m.viewport.SetWidth(mainW)
	m.viewport.SetHeight(vpH)

	taW := mainW - 4
	if taW < 10 {
		taW = 10
	}
	m.textarea.SetWidth(taW)
}

func (m *Model) refreshViewport() {
	cur := m.manager.Current()
	if cur == nil {
		m.viewport.SetContent(m.styles.Hint.Render("ninguna sesión activa — ctrl+n para crear"))
		return
	}
	w := m.viewport.Width()
	if w <= 0 {
		w = m.width
	}
	if len(cur.Items) == 0 && cur.State == session.StateIdle {
		m.viewport.SetContent(m.welcomeText())
		return
	}
	ctx := RenderContext{
		Width:     w,
		Styles:    m.styles,
		LiveFrame: m.spinner.View(),
		Markdown:  m.markdown,
	}
	m.viewport.SetContent(RenderTranscript(cur.Items, ctx))
}

func (m *Model) welcomeText() string {
	s := m.styles
	bullet := s.Hint.Render("•")
	row := func(k, d string) string {
		return bullet + " " + s.StatusKey.Render(k) + " " + s.Hint.Render(d)
	}
	return strings.Join([]string{
		s.HeaderLogo.Render("☀ sunnytui"),
		"",
		s.AssistantText.Render("escribe un mensaje para empezar a chatear con claude code"),
		"",
		row("ctrl+n", "añade una sesión en otro directorio"),
		row("ctrl+r", "renombra la sesión activa"),
		row("tab   ", "cambia entre sesiones"),
		row("ctrl+w", "cierra la sesión actual"),
		row("ctrl+c", "limpia el input (×2 cancela el turno)"),
		row("esc   ", "sale de sunnytui"),
		"",
		row("ctrl+j / alt+enter", "nueva línea"),
		row("ctrl+←/→", "saltar palabras"),
		row("ctrl+del", "borrar palabra"),
	}, "\n")
}

func (m *Model) markdown(text string) string {
	w := m.viewport.Width() - 4
	if w < 20 {
		w = 20
	}
	if m.md == nil || m.mdW != w {
		m.mdCache = nil
		r, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle("dark"),
			glamour.WithWordWrap(w),
			glamour.WithEmoji(),
		)
		if err != nil {
			return wrap(text, w)
		}
		m.md = r
		m.mdW = w
	}
	if m.mdCache == nil {
		m.mdCache = map[string]string{}
	}
	if cached, ok := m.mdCache[text]; ok {
		return cached
	}
	out, err := m.md.Render(text)
	if err != nil {
		return wrap(text, w)
	}
	rendered := strings.TrimRight(out, "\n")
	m.mdCache[text] = rendered
	return rendered
}

func (m Model) Run(ctx context.Context) error {
	// AltScreen + MouseMode are now declared in View() (declarative model in v2).
	p := tea.NewProgram(m,
		tea.WithContext(ctx),
		tea.WithFilter(mouseEventFilter),
	)
	go func() {
		<-ctx.Done()
		_ = time.AfterFunc(0, p.Quit)
	}()
	_, err := p.Run()
	if m.manager != nil {
		m.manager.CloseAll()
	}
	return err
}

// mouseEventFilter throttles high-frequency mouse motion / wheel events to at
// most one per 15ms. Crush uses the same trick to keep heavy scroll-wheel
// activity from saturating the program loop.
var lastMouseEvent time.Time

func mouseEventFilter(_ tea.Model, msg tea.Msg) tea.Msg {
	switch msg.(type) {
	case tea.MouseWheelMsg, tea.MouseMotionMsg:
		now := time.Now()
		if now.Sub(lastMouseEvent) < 15*time.Millisecond {
			return nil
		}
		lastMouseEvent = now
	}
	return msg
}
