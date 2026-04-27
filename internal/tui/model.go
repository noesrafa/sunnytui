package tui

import (
	"context"
	"image"
	"io"
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
	"github.com/atotto/clipboard"

	"github.com/noesrafa/sunnytui/internal/anim"
	"github.com/noesrafa/sunnytui/internal/highlight"
	"github.com/noesrafa/sunnytui/internal/runs"
	"github.com/noesrafa/sunnytui/internal/session"
	"github.com/noesrafa/sunnytui/internal/state"
	"github.com/noesrafa/sunnytui/internal/terminal"
)

// activeKind identifies which tab type is currently in focus.
type activeKind int

const (
	activeClaude activeKind = iota
	activePane
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
	runs          *runs.Manager
	panes         *terminal.Manager
	activeKind    activeKind
	selectMode    bool // legacy escape hatch: drops mouse capture so terminal does native selection
	overlay       *Overlay
	initialCwd    string
	defaultModel  string
	defaultEffort string
	themeID       string // active theme; persisted in state.json
	skipPerms     bool
	lastErr       error
	lastCtrlC     time.Time

	// App-level drag-to-select-and-copy (Crush-style). Coords are in
	// rendered-transcript content space (X = column, Y = line within the
	// full SetContent string, NOT the on-screen row).
	mouseDown      bool
	selStart       image.Point
	selEnd         image.Point
	lastTranscript string // unhighlighted content used for clipboard extract

	viewport viewport.Model
	textarea textarea.Model
	spinner  spinner.Model

	md      *glamour.TermRenderer
	mdW     int
	mdCache map[string]string

	// Morphing-string spinner shown during assistant streaming. One global
	// instance — the active session's "Thinking" label drives it.
	thinkingAnim *anim.Anim

	// logoFrame is the gradient-sweep counter. Driven by logoTickMsg, read
	// by renderLogo to shift the per-column color ramp each tick.
	logoFrame int
}

type Options struct {
	Logger                   *log.Logger
	DefaultModel             string
	DefaultEffort            string
	DangerousSkipPermissions bool
	Runs                     *runs.Manager
	Panes                    *terminal.Manager
	// InitialActiveKind ("claude" | "pane") restores which kind of tab was
	// focused at the previous shutdown. Empty defaults to claude.
	InitialActiveKind string
	// InitialTheme is the persisted theme ID from state.json. Empty falls
	// back to the default (Themes[0]).
	InitialTheme string
}

const (
	headerHeight   = 1
	statusHeight   = 1
	textareaMinH   = 3
	textareaMaxH   = 12
	ctrlCDoubleWin = 1500 * time.Millisecond
	// mdCacheMax bounds the markdown render cache. Past this size we drop the
	// whole map — long sessions otherwise grow it forever.
	mdCacheMax = 512
	// inputTopGap is the number of empty rows reserved above the input
	// box so the assistant attribution row doesn't sit flush against the
	// input border. Reserved in layout() and rendered in renderMain().
	inputTopGap = 4
)

func NewModel(ctx context.Context, mgr *session.Manager, initialCwd string, opts Options) Model {
	// Apply the persisted theme before building styles so every Style picks
	// up the right palette on first render. Unknown IDs fall back to the
	// default theme.
	theme := ThemeByID(opts.InitialTheme)
	SetPalette(theme.P)
	st := DefaultStyles()
	km := DefaultKeyMap()

	logger := opts.Logger
	if logger == nil {
		logger = log.NewWithOptions(io.Discard, log.Options{})
	}

	// Editor — patterned exactly off Crush's textarea init at
	// /tmp/charm-crush/internal/ui/model/ui.go:271. Same init order, same
	// style application, same dynamic height, same cursor block + blink.
	ta := textarea.New()
	ta.SetStyles(st.EditorTextarea)
	ta.Placeholder = "escribe tu mensaje y enter para enviar (ctrl+j newline · \\+enter también)"
	ta.Prompt = "› "
	ta.CharLimit = -1
	ta.ShowLineNumbers = false
	// Virtual cursor: renders inline as a styled cell inside the textarea
	// View() output. Sidesteps the real-cursor offset math (Crush relies on
	// ultraviolet's exact layout coords; we don't have that machinery yet).
	ta.SetVirtualCursor(true)
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
	// SoftWrap = chat content always wraps to viewport width instead of
	// allowing horizontal scroll. Long code lines, ASCII tables, etc.
	// would otherwise let the user scroll the chat sideways and break
	// the layout. This is the safety net beneath our manual wrappers
	// (glamour WordWrap, lipgloss width-aware renders).
	vp.SoftWrap = true
	// In a chat we can't have the viewport eating letter keys: the
	// default KeyMap treats j/k/h/l/u/d/b/f/space as scroll commands,
	// so typing words like "kubernetes" or "javascript" would pan the
	// transcript while you wrote them. Replace the default KeyMap with
	// only PgUp/PgDn — same idea Crush uses for its log dialog
	// (internal/ui/dialog/permissions.go) but stricter, since textarea
	// owns the rest. Mouse wheel is handled separately in updateMouse.
	disabled := key.NewBinding(key.WithDisabled())
	vp.KeyMap = viewport.KeyMap{
		PageUp:       key.NewBinding(key.WithKeys("pgup")),
		PageDown:     key.NewBinding(key.WithKeys("pgdown")),
		HalfPageUp:   disabled,
		HalfPageDown: disabled,
		Up:           disabled,
		Down:         disabled,
		Left:         disabled,
		Right:        disabled,
	}

	thinking := anim.New(anim.Settings{
		Size:       12,
		GradFrom:   colSecondary, // Dolly magenta
		GradTo:     colPrimary,   // Charple purple
		LabelColor: colText,      // Ash off-white
	})

	defModel := opts.DefaultModel
	if defModel == "" {
		defModel = "opus"
	}
	defEffort := opts.DefaultEffort
	if defEffort == "" {
		defEffort = "max"
	}

	startKind := activeClaude
	if opts.InitialActiveKind == "pane" && opts.Panes != nil && opts.Panes.Len() > 0 {
		startKind = activePane
	}
	return Model{
		ctx:           ctx,
		styles:        st,
		keymap:        km,
		logger:        logger,
		manager:       mgr,
		runs:          opts.Runs,
		panes:         opts.Panes,
		activeKind:    startKind,
		overlay:       &Overlay{},
		viewport:      vp,
		textarea:      ta,
		spinner:       sp,
		thinkingAnim:  thinking,
		initialCwd:    initialCwd,
		defaultModel:  defModel,
		defaultEffort: defEffort,
		themeID:       theme.ID,
		skipPerms:     opts.DangerousSkipPermissions,
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{textarea.Blink, branchTickCmd(), logoTickCmd()}
	if m.anyThinking() {
		cmds = append(cmds, m.spinner.Tick)
	}
	for _, s := range m.manager.Sessions {
		cmds = append(cmds, waitForSession(s))
	}
	// CRITICAL: restored panes (loaded from ~/.sunnytui/panes.json) need
	// their PTY-read loop kicked off too — otherwise the buffer fills, the
	// child process blocks on write, and the pane appears dead.
	if m.panes != nil {
		for _, p := range m.panes.Panes {
			cmds = append(cmds, readPaneCmd(p))
		}
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

// paneSize returns the (cols, rows) the active pane should occupy in the
// main column when it is the visible tab.
func (m Model) paneSize() (int, int) {
	w := m.width - sidebarWidth - sidebarGap
	if w < 20 {
		w = 20
	}
	h := m.height - statusHeight
	if h < 5 {
		h = 5
	}
	return w, h
}

// readPaneCmd reads one chunk from the pane's PTY (which feeds vt10x
// internally) and returns a paneOutputMsg so the Update loop re-renders.
// On EOF / error, returns paneClosedMsg.
func readPaneCmd(p *terminal.Pane) tea.Cmd {
	id := p.ID
	return func() tea.Msg {
		buf := make([]byte, 4096)
		_, err := p.ReadOnce(buf)
		if err != nil {
			return paneClosedMsg{PaneID: id, Err: err}
		}
		return paneOutputMsg{PaneID: id}
	}
}

// collectTiles assembles the picker entries: every claude session, then
// every terminal pane. Marks the currently-focused one.
func (m Model) collectTiles() []TileItem {
	var items []TileItem
	for i, s := range m.manager.Sessions {
		items = append(items, TileItem{
			Kind:   "claude",
			Index:  i,
			Label:  s.Title,
			Detail: s.Cwd,
			Active: m.activeKind == activeClaude && i == m.manager.Active,
		})
	}
	if m.panes != nil {
		for i, p := range m.panes.Panes {
			items = append(items, TileItem{
				Kind:   "pane",
				Index:  i,
				Label:  p.Title,
				Detail: p.Command,
				Active: m.activeKind == activePane && i == m.panes.Active,
			})
		}
	}
	return items
}

// activePane returns the currently focused pane, or nil if no pane tab is
// active.
func (m Model) activePane() *terminal.Pane {
	if m.activeKind != activePane || m.panes == nil {
		return nil
	}
	return m.panes.Current()
}

// cycleTab moves the focus to the next/prev tab in the unified flat list of
// (claude sessions + panes). Wraps. Resizes the destination pane on entry.
func (m *Model) cycleTab(by int) {
	clCount := m.manager.Len()
	pnCount := 0
	if m.panes != nil {
		pnCount = m.panes.Len()
	}
	total := clCount + pnCount
	if total == 0 {
		return
	}
	cur := m.flatTabIndex()
	next := ((cur+by)%total + total) % total
	m.setFlatTabIndex(next)
	// On switching to a pane, ensure it matches current main dims and the
	// child got SIGWINCH if anything changed.
	if p := m.activePane(); p != nil {
		w, h := m.paneSize()
		p.Resize(w, h)
	}
	// On switching to a claude session, restore its draft.
	if m.activeKind == activeClaude {
		if cur := m.manager.Current(); cur != nil {
			m.textarea.SetValue(cur.Draft)
			m.textarea.CursorEnd()
			m.layout()
			m.refreshViewport()
			m.viewport.GotoBottom()
		}
	}
}

func (m Model) flatTabIndex() int {
	if m.activeKind == activeClaude {
		return m.manager.Active
	}
	if m.panes == nil {
		return 0
	}
	return m.manager.Len() + m.panes.Active
}

func (m *Model) setFlatTabIndex(i int) {
	clCount := m.manager.Len()
	if i < clCount {
		// Save current draft before moving away from a claude session.
		if m.activeKind == activeClaude {
			if cur := m.manager.Current(); cur != nil {
				cur.Draft = m.textarea.Value()
			}
		}
		m.activeKind = activeClaude
		m.manager.Active = i
		return
	}
	if m.panes == nil {
		return
	}
	if m.activeKind == activeClaude {
		if cur := m.manager.Current(); cur != nil {
			cur.Draft = m.textarea.Value()
		}
	}
	m.activeKind = activePane
	m.panes.SetActive(i - clCount)
}

// inChatRegion reports whether (x, y) — in screen coords — falls inside the
// transcript viewport: right of the sidebar, below the header, above the
// input box. Pane mode and overlays are excluded by callers.
func (m Model) inChatRegion(x, y int) bool {
	if x < sidebarWidth+sidebarGap || x >= m.width {
		return false
	}
	if y < headerHeight {
		return false
	}
	if y >= headerHeight+m.viewport.Height() {
		return false
	}
	return true
}

// screenToContent maps screen coordinates into transcript content
// coordinates (X = column inside the rendered string, Y = line inside the
// full SetContent string accounting for viewport scroll). Coords are
// clamped to the chat content rectangle so callers can use the result
// even when the user drags beyond the viewport.
func (m Model) screenToContent(x, y int) (cx, cy int) {
	cx = x - sidebarWidth - sidebarGap
	if cx < 0 {
		cx = 0
	}
	if w := m.viewport.Width(); w > 0 && cx >= w {
		cx = w - 1
	}
	cy = (y - headerHeight) + m.viewport.YOffset()
	if cy < 0 {
		cy = 0
	}
	return
}

func (m Model) hasSelection() bool {
	return m.selStart != m.selEnd
}

func (m *Model) clearSelection() {
	m.mouseDown = false
	m.selStart = image.Point{}
	m.selEnd = image.Point{}
}

// selectionRange returns the selection coords normalized so (sl, sc) precedes
// (el, ec) in reading order. Mirrors Crush's getHighlightRange.
func (m Model) selectionRange() (sl, sc, el, ec int) {
	a, b := m.selStart, m.selEnd
	if a.Y > b.Y || (a.Y == b.Y && a.X > b.X) {
		a, b = b, a
	}
	return a.Y, a.X, b.Y, b.X
}

// handleMouse implements drag-to-select-and-copy on the transcript. Mouse
// down starts a selection at the cursor; motion extends it; release copies
// the selected text to the clipboard (or clears the empty selection if it
// was just a click).
func (m Model) handleMouse(mm tea.MouseMsg) Model {
	e := mm.Mouse()
	cx, cy := m.screenToContent(e.X, e.Y)
	switch ev := mm.(type) {
	case tea.MouseClickMsg:
		if ev.Button != tea.MouseLeft {
			return m
		}
		if !m.inChatRegion(e.X, e.Y) {
			m.clearSelection()
			m.refreshViewport()
			return m
		}
		m.mouseDown = true
		m.selStart = image.Pt(cx, cy)
		m.selEnd = image.Pt(cx, cy)
		m.refreshViewport()
	case tea.MouseMotionMsg:
		if !m.mouseDown {
			return m
		}
		m.selEnd = image.Pt(cx, cy)
		m.refreshViewport()
	case tea.MouseReleaseMsg:
		if !m.mouseDown {
			return m
		}
		m.mouseDown = false
		m.selEnd = image.Pt(cx, cy)
		if text := m.copySelection(); text == "" {
			m.clearSelection()
		}
		m.refreshViewport()
	}
	return m
}

// copySelection extracts the selected text from the last rendered
// transcript and writes it to the clipboard. Returns the extracted text
// (or "" if the selection was empty).
func (m Model) copySelection() string {
	if !m.hasSelection() || m.lastTranscript == "" {
		return ""
	}
	sl, sc, el, ec := m.selectionRange()
	w := m.viewport.Width()
	h := lipgloss.Height(m.lastTranscript)
	text := highlight.Extract(m.lastTranscript, w, h, sl, sc, el, ec)
	if text == "" {
		return ""
	}
	if err := clipboard.WriteAll(text); err != nil {
		m.logger.Warn("clipboard write failed", "err", err, "len", len(text))
	} else {
		m.logger.Info("clipboard write", "len", len(text))
	}
	return text
}

// anyRunRunning is true while at least one registered run has its child
// process alive. Used to keep the spinner tick chain alive so the logs
// viewer refreshes in real time.
func (m Model) anyRunRunning() bool {
	if m.runs == nil {
		return false
	}
	for _, r := range m.runs.All() {
		if r.Running() {
			return true
		}
	}
	return false
}

// Update is the Bubble Tea entry point. It dispatches into focused
// sub-handlers (one per major message family) and ends with a fallthrough
// that lets the textarea + viewport see whatever no specific handler claimed.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if next, cmd, handled := m.updateAppMsg(msg); handled {
		return next, cmd
	}
	if next, cmd, handled := m.updateMouse(msg); handled {
		return next, cmd
	}

	var cmds []tea.Cmd
	if m.overlay.HasOpen() {
		if _, isKey := msg.(tea.KeyMsg); isKey {
			return m, m.overlay.UpdateTop(msg)
		}
		cmds = append(cmds, m.overlay.UpdateTop(msg))
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.applyResize(msg)
	case tea.KeyMsg:
		next, cmd, handled := m.updateKey(msg)
		if handled {
			return next, cmd
		}
		m = next
		cmds = append(cmds, cmd)
	case sessionEventMsg:
		cmds = append(cmds, m.handleSessionEvent(msg))
	case sessionClosedMsg:
		m.manager.Close(msg.SessionID)
		m.refreshViewport()
	case spinner.TickMsg:
		next, cmd := m.handleSpinnerTick(msg)
		m = next
		cmds = append(cmds, cmd)
	case anim.StepMsg:
		cmds = append(cmds, m.handleAnimStep(msg))
	case branchTickMsg:
		cmds = append(cmds, m.handleBranchTick())
	case logoTickMsg:
		m.logoFrame++
		cmds = append(cmds, logoTickCmd())
	}

	if !m.overlay.HasOpen() {
		var cmd tea.Cmd
		prevValue := m.textarea.Value()
		// Snapshot whether the user was reading the latest message
		// BEFORE the textarea consumes the key. If they were pinned to
		// bottom and typing grows the textarea, we want to stay pinned
		// after layout shrinks the viewport; if they had scrolled up
		// to read history, we leave their position alone.
		wasAtBottom := m.viewport.AtBottom()
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
		if m.textarea.Value() != prevValue {
			m.layout() // dynamic textarea height
			if wasAtBottom {
				m.viewport.GotoBottom()
			}
		}
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// updateAppMsg handles in-app messages emitted by dialogs and other
// sub-models (close/quit/run/pane/session creation, etc.). It owns its
// messages — when handled is true the caller MUST return immediately.
func (m Model) updateAppMsg(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch v := msg.(type) {
	case CloseDialogMsg:
		m.overlay.CloseTop()
		return m, nil, true
	case ConfirmQuitMsg:
		return m, tea.Quit, true
	case OpenRunEditMsg:
		cwd := m.initialCwd
		if cur := m.manager.Current(); cur != nil {
			cwd = cur.Cwd
		}
		return m, m.overlay.Open(NewRunEditDialog(cwd, m.styles)), true
	case OpenRunLogsMsg:
		if m.runs != nil {
			if r := m.runs.Get(v.ID); r != nil {
				return m, m.overlay.Open(NewRunLogsDialog(r, m.styles)), true
			}
		}
		return m, nil, true
	case CreateRunMsg:
		m.overlay.CloseTop()
		if m.runs == nil {
			return m, nil, true
		}
		r := m.runs.Add(v.Name, v.Command, v.Cwd)
		if err := m.runs.Save(); err != nil {
			m.logger.Warn("save runs", "err", err)
		}
		m.logger.Info("run created", "id", r.ID, "name", r.Name)
		// Reopen the runs list so the user can act on the new entry.
		return m, m.overlay.Open(NewRunsDialog(m.runs, m.styles)), true
	case DeleteRunMsg:
		if m.runs != nil {
			m.runs.Remove(v.ID)
			if err := m.runs.Save(); err != nil {
				m.logger.Warn("save runs", "err", err)
			}
		}
		return m, nil, true
	case CreatePaneMsg:
		return m.createPane(v)
	case ClosePaneMsg:
		if m.panes != nil {
			m.panes.Close(v.ID)
			if m.panes.Len() == 0 {
				m.activeKind = activeClaude
			}
			m.saveState()
		}
		return m, nil, true
	case SwitchTabMsg:
		m.overlay.CloseTop()
		m.switchToTab(v.Kind, v.Index)
		return m, nil, true
	case paneOutputMsg:
		// vt10x already received the bytes inside ReadOnce; just re-arm the
		// reader so the next chunk gets pumped.
		if m.panes != nil {
			if p := m.panes.ByID(v.PaneID); p != nil {
				return m, readPaneCmd(p), true
			}
		}
		return m, nil, true
	case paneClosedMsg:
		if m.panes != nil {
			m.panes.Close(v.PaneID)
			if m.panes.Len() == 0 {
				m.activeKind = activeClaude
			}
		}
		return m, nil, true
	case CreateSessionMsg:
		return m.createSession(v)
	case RenameSessionMsg:
		m.overlay.CloseTop()
		if cur := m.manager.Current(); cur != nil {
			cur.Title = v.NewTitle
			m.logger.Info("session renamed", "session", cur.ID, "title", v.NewTitle)
		}
		m.saveState()
		return m, nil, true
	case PreviewThemeMsg:
		// Live preview while user navigates the picker. Repaint only —
		// don't close or persist; the dialog still owns the decision.
		m.repaint(v.ID)
		return m, nil, true
	case ApplyThemeMsg:
		// User pressed enter — commit the choice.
		m.overlay.CloseTop()
		m.repaint(v.ID)
		m.saveState()
		m.logger.Info("theme applied", "id", v.ID)
		return m, nil, true
	case CancelSettingsMsg:
		// User pressed esc — roll back to whatever was active before
		// they opened the dialog.
		m.overlay.CloseTop()
		m.repaint(v.OriginalID)
		return m, nil, true
	}
	return m, nil, false
}

// repaint swaps the active palette and re-applies it everywhere a Style
// got copied at construction time. Called by all three settings flows
// (preview, apply, cancel); only apply also closes the overlay and saves.
//
//   - m.styles is fully rebuilt from the new palette globals.
//   - The textarea owns its own Styles struct; without re-applying it,
//     the cursor color / placeholder color / prompt color stick to the
//     previous theme until a restart.
//   - The spinner stores a Style on its struct, so it's re-set explicitly.
//   - The morphing-thinking anim stores its gradient colors as fields;
//     SetColors swaps them without resetting the morph state.
//   - Glamour bakes ANSI codes into its output; m.md + m.mdCache are
//     thrown away so transcripts re-render against the new palette.
func (m *Model) repaint(id string) {
	t := ThemeByID(id)
	SetPalette(t.P)
	m.styles = DefaultStyles()
	m.themeID = t.ID

	m.textarea.SetStyles(m.styles.EditorTextarea)
	m.spinner.Style = lipgloss.NewStyle().Foreground(colWarning)
	if m.thinkingAnim != nil {
		m.thinkingAnim.SetColors(colSecondary, colPrimary, colText)
	}

	m.md = nil
	m.mdCache = nil
	m.refreshViewport()
}

func (m Model) createPane(v CreatePaneMsg) (Model, tea.Cmd, bool) {
	m.overlay.CloseTop()
	if m.panes == nil {
		m.panes = terminal.NewManager()
	}
	mainW, mainH := m.paneSize()
	p, err := terminal.Spawn(v.Name, v.Command, v.Cwd, mainW, mainH)
	if err != nil {
		m.lastErr = err
		m.logger.Error("spawn pane failed", "err", err, "cmd", v.Command)
		return m, nil, true
	}
	m.panes.Add(p)
	m.activeKind = activePane
	m.saveState()
	m.logger.Info("pane spawned", "id", p.ID, "name", p.Title, "cmd", p.Command)
	return m, readPaneCmd(p), true
}

func (m Model) createSession(v CreateSessionMsg) (Model, tea.Cmd, bool) {
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
		m.logger.Error("create session failed", "err", err, "cwd", v.Cwd)
		return m, nil, true
	}
	m.manager.Add(s)
	m.textarea.Reset() // new session starts with empty draft
	m.layout()
	m.refreshViewport()
	m.saveState()
	return m, waitForSession(s), true
}

func (m *Model) switchToTab(kind string, index int) {
	switch kind {
	case "claude":
		m.activeKind = activeClaude
		m.manager.Active = index
		if cur := m.manager.Current(); cur != nil {
			m.textarea.SetValue(cur.Draft)
			m.textarea.CursorEnd()
			m.layout()
			m.refreshViewport()
			m.viewport.GotoBottom()
		}
	case "pane":
		if m.panes != nil {
			m.activeKind = activePane
			m.panes.SetActive(index)
			if p := m.activePane(); p != nil {
				w, h := m.paneSize()
				p.Resize(w, h)
			}
		}
	}
}

// updateMouse routes mouse events. Wheel scrolls the viewport; click/motion
// /release drive the app-level drag-to-select-and-copy. Returns handled=true
// only for genuine MouseMsg values — other messages fall through unchanged.
func (m Model) updateMouse(msg tea.Msg) (Model, tea.Cmd, bool) {
	if mm, isWheel := msg.(tea.MouseWheelMsg); isWheel {
		// Drop horizontal wheel events outright — trackpads emit them
		// on lateral swipes and shift+wheel, both of which would scroll
		// the chat sideways since SoftWrap can't help once the viewport
		// has accepted an xOffset. Keep vertical wheel for scrolling.
		if mm.Button == tea.MouseWheelLeft || mm.Button == tea.MouseWheelRight {
			return m, nil, true
		}
		if mm.Mod.Contains(tea.ModShift) {
			return m, nil, true
		}
		var cmd tea.Cmd
		if !m.overlay.HasOpen() {
			m.viewport, cmd = m.viewport.Update(mm)
		}
		return m, cmd, true
	}
	if mm, ok := msg.(tea.MouseMsg); ok {
		// In overlays, pane mode, or when the user has explicitly switched
		// to terminal-native selection, our app-level handler stays out of
		// the way.
		if m.overlay.HasOpen() || m.activeKind == activePane || m.selectMode {
			return m, nil, true
		}
		return m.handleMouse(mm), nil, true
	}
	return m, nil, false
}

func (m *Model) applyResize(msg tea.WindowSizeMsg) {
	m.width, m.height = msg.Width, msg.Height
	m.layout()
	m.md = nil
	m.mdCache = nil
	m.refreshViewport()
	// Resize the active pane (if any) so the embedded child gets SIGWINCH.
	if p := m.activePane(); p != nil {
		w, h := m.paneSize()
		p.Resize(w, h)
	}
	m.ready = true
}

// updateKey is the tea.KeyMsg dispatcher: master shortcuts → pane-forwarding
// → claude-session keys. Returns handled=true when the key was consumed; when
// false, the dispatcher falls through to textarea/viewport so character input
// reaches the editor.
func (m Model) updateKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	// MASTER shortcuts always take precedence — even when a pane is
	// active they're how the user navigates back out / opens dialogs.
	switch {
	case key.Matches(msg, m.keymap.Quit):
		return m, m.openQuitDialog(), true
	case key.Matches(msg, m.keymap.NewPane):
		return m, m.overlay.Open(NewNewPaneDialog(m.initialCwd, m.styles)), true
	case key.Matches(msg, m.keymap.TilePicker):
		return m, m.overlay.Open(NewTilePickerDialog(m.collectTiles(), m.styles)), true
	case key.Matches(msg, m.keymap.SelectMode):
		// Toggle mouse capture: when off, the terminal regains native
		// drag-to-select. The View() reads m.selectMode each frame and
		// sets MouseMode accordingly.
		m.selectMode = !m.selectMode
		return m, nil, true
	case key.Matches(msg, m.keymap.Settings):
		return m, m.overlay.Open(NewSettingsDialog(m.themeID, m.styles)), true
	case key.Matches(msg, m.keymap.NewSession):
		return m, m.overlay.Open(NewNewSessionDialog(m.initialCwd, m.defaultModel, m.defaultEffort, m.styles)), true
	case key.Matches(msg, m.keymap.Runs):
		if m.runs == nil {
			return m, nil, true
		}
		return m, tea.Batch(m.overlay.Open(NewRunsDialog(m.runs, m.styles)), m.spinner.Tick), true
	case key.Matches(msg, m.keymap.NextSession):
		m.cycleTab(1)
		return m, nil, true
	case key.Matches(msg, m.keymap.PrevSession):
		m.cycleTab(-1)
		return m, nil, true
	}

	// If a terminal pane has focus, all other keys are forwarded to its
	// PTY (translated to xterm bytes). The pane-only Esc/Ctrl+W still
	// fall through master switch above.
	if p := m.activePane(); p != nil {
		if pk, ok := msg.(tea.KeyPressMsg); ok {
			if b := terminal.KeyToBytes(pk); b != nil {
				if err := p.Send(b); err != nil {
					m.logger.Debug("pane send failed", "err", err, "pane", p.ID)
				}
			}
		}
		return m, nil, true
	}

	// Below: claude-session-only keys.
	switch {
	case key.Matches(msg, m.keymap.ClearOrCancel):
		m.handleClearOrCancel()
		return m, nil, true
	case key.Matches(msg, m.keymap.Rename):
		if cur := m.manager.Current(); cur != nil {
			return m, m.overlay.Open(NewRenameDialog(cur.Title, m.styles)), true
		}
		return m, nil, true
	case key.Matches(msg, m.keymap.CloseSession):
		m.handleCloseTab()
		return m, nil, true
	case key.Matches(msg, m.keymap.Send):
		next, cmd := m.handleSend()
		return next, cmd, true
	}
	return m, nil, false
}

func (m *Model) handleClearOrCancel() {
	if !m.lastCtrlC.IsZero() && time.Since(m.lastCtrlC) < ctrlCDoubleWin {
		// Second press → cancel current turn.
		m.lastCtrlC = time.Time{}
		cur := m.manager.Current()
		if cur != nil && cur.State == session.StateThinking {
			if err := cur.Cancel(); err != nil {
				cur.LastErr = err
			}
		}
		return
	}
	// First press → clear textarea, start the double-press timer.
	m.textarea.Reset()
	if cur := m.manager.Current(); cur != nil {
		cur.Draft = ""
	}
	m.lastCtrlC = time.Now()
	m.layout()
}

func (m *Model) handleCloseTab() {
	if m.activeKind == activePane && m.panes != nil {
		if p := m.panes.Current(); p != nil {
			m.panes.Close(p.ID)
			if m.panes.Len() == 0 {
				m.activeKind = activeClaude
			}
		}
		m.saveState()
		return
	}
	cur := m.manager.Current()
	if cur != nil {
		m.manager.Close(cur.ID)
		if next := m.manager.Current(); next != nil {
			m.textarea.SetValue(next.Draft)
			m.textarea.CursorEnd()
		} else {
			m.textarea.Reset()
		}
		m.layout()
		m.refreshViewport()
	}
	m.saveState()
}

func (m Model) handleSend() (Model, tea.Cmd) {
	cur := m.manager.Current()
	if cur == nil || cur.State != session.StateIdle {
		return m, nil
	}
	value := m.textarea.Value()
	// Crush pattern: trailing backslash escapes Enter to a newline (so users
	// can compose multiline messages without Shift+Enter).
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
	// Kick off both the spinner tick chain and the morphing-string anim.
	// Each fires its own re-arm message; they die when the session goes back
	// to idle.
	return m, tea.Batch(m.spinner.Tick, m.thinkingAnim.Step())
}

func (m *Model) handleSessionEvent(msg sessionEventMsg) tea.Cmd {
	sess := m.manager.ByID(msg.SessionID)
	if sess == nil {
		return nil
	}
	wasThinking := sess.State == session.StateThinking
	sess.HandleEvent(msg.Event)
	if cur := m.manager.Current(); cur != nil && cur.ID == sess.ID {
		m.refreshViewport()
		m.viewport.GotoBottom()
	}
	// Persist after each turn so a crash mid-session doesn't lose the
	// transcript. The turn boundary is the state.Idle transition.
	if wasThinking && sess.State == session.StateIdle {
		m.saveState()
	}
	return waitForSession(sess)
}

// handleSpinnerTick keeps ticking while any session is thinking OR any run
// is alive (so the logs viewer auto-tails) OR a modal that wants live data
// is open.
func (m Model) handleSpinnerTick(msg spinner.TickMsg) (Model, tea.Cmd) {
	if !m.anyThinking() && !m.anyRunRunning() && !m.overlay.HasOpen() {
		return m, nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	if cur := m.manager.Current(); cur != nil && cur.State == session.StateThinking {
		m.refreshViewport()
	}
	return m, cmd
}

// branchTickCmd schedules the next branch poll. Cheap (one `git -C cwd
// branch --show-current` per session every few seconds), so the input-hint
// row reflects checkouts done outside the TUI in near real time.
func branchTickCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return branchTickMsg{} })
}

// logoTickCmd drives the brand-mark gradient sweep. 120ms cadence keeps
// the animation visible without saturating the program loop on idle
// terminals. Each tick increments Model.logoFrame and re-arms itself.
func logoTickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return logoTickMsg{} })
}

func (m *Model) handleBranchTick() tea.Cmd {
	changed := false
	for _, s := range m.manager.Sessions {
		if s.RefreshBranch() {
			changed = true
		}
	}
	if changed {
		m.refreshViewport()
	}
	return branchTickCmd()
}

func (m *Model) handleAnimStep(msg anim.StepMsg) tea.Cmd {
	if msg.ID != m.thinkingAnim.ID() {
		return nil
	}
	m.thinkingAnim.Tick()
	if !m.anyThinking() {
		return nil
	}
	m.refreshViewport()
	return m.thinkingAnim.Step()
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
	mainW := m.width - sidebarWidth - sidebarGap
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
	gapH := inputTopGap  // breathing room above the input box
	vpH := bodyH - inputBoxH - hintH - gapH
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
		ModelName: cur.Model,
	}
	out := RenderTranscript(cur.Items, ctx)
	// Trailing morphing spinner while the assistant is responding.
	if cur.State == session.StateThinking {
		label := cur.LiveStatus() // "thinking" / "writing" / "running ToolName"
		if label == "" {
			label = "thinking"
		}
		m.thinkingAnim.SetLabel(label)
		spinnerLine := m.styles.AssistantMsgBlurred.Render(m.thinkingAnim.Render())
		if out != "" {
			out += "\n\n"
		}
		out += spinnerLine
	}
	// Cache the unhighlighted version so copySelection can extract plain
	// text from the same string the user is looking at.
	m.lastTranscript = out
	if m.hasSelection() {
		sl, sc, el, ec := m.selectionRange()
		out = highlight.Apply(out, m.viewport.Width(), lipgloss.Height(out), sl, sc, el, ec)
	}
	m.viewport.SetContent(out)
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
	if len(m.mdCache) >= mdCacheMax {
		// Cheap eviction: drop everything once the cache fills up. Re-renders
		// will warm it again over the next few frames.
		m.mdCache = map[string]string{}
	}
	m.mdCache[text] = rendered
	return rendered
}

func (m Model) Run(ctx context.Context) error {
	p := tea.NewProgram(m,
		tea.WithContext(ctx),
		tea.WithFilter(mouseEventFilter),
	)
	go func() {
		<-ctx.Done()
		p.Quit()
	}()
	finalModel, err := p.Run()
	// Persist before tearing down sessions so we capture the final draft.
	if fm, ok := finalModel.(Model); ok {
		fm.saveState()
	} else {
		m.saveState()
	}
	if m.manager != nil {
		m.manager.CloseAll()
	}
	return err
}

// saveState snapshots EVERYTHING (sessions + panes + active tab) to
// ~/.sunnytui/state.json. Called once at shutdown so the next run boots back
// into the same layout.
func (m Model) saveState() {
	if m.manager == nil {
		return
	}
	// Capture the in-flight textarea content into the current session's draft
	// so it survives across restarts.
	if cur := m.manager.Current(); cur != nil && m.activeKind == activeClaude {
		cur.Draft = m.textarea.Value()
	}
	var sessions []state.SavedSession
	for _, s := range m.manager.Sessions {
		// Marshal the transcript so it survives restart. Errors here would
		// only happen if a new Item type was added without a marshaller —
		// log but don't drop the session metadata.
		raw, mErr := session.MarshalItems(s.Items)
		if mErr != nil {
			m.logger.Warn("marshal items", "session", s.ID, "err", mErr)
			raw = nil
		}
		sessions = append(sessions, state.SavedSession{
			Title:     s.Title,
			Cwd:       s.Cwd,
			Model:     s.Model,
			Effort:    m.defaultEffort,
			Draft:     s.Draft,
			RemoteID:  s.RemoteID,
			Items:     raw,
			TotalCost: s.TotalCost,
			Turns:     s.Turns,
		})
	}
	var panes []state.SavedPane
	if m.panes != nil {
		for _, p := range m.panes.Panes {
			panes = append(panes, state.SavedPane{
				Title:   p.Title,
				Command: p.Command,
				Cwd:     p.Cwd,
			})
		}
	}
	kind := "claude"
	idx := m.manager.Active
	if m.activeKind == activePane && m.panes != nil {
		kind = "pane"
		idx = m.panes.Active
	}
	st := &state.State{
		Sessions:   sessions,
		Panes:      panes,
		ActiveKind: kind,
		ActiveIdx:  idx,
		Theme:      m.themeID,
	}
	if err := state.Save(st); err != nil {
		m.logger.Error("save state failed", "err", err)
	} else {
		m.logger.Info("state saved", "sessions", len(sessions), "panes", len(panes), "kind", kind, "idx", idx)
	}
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
