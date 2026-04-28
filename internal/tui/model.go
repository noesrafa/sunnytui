package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"github.com/atotto/clipboard"

	"github.com/noesrafa/sunnytui/internal/anim"
	imgclip "github.com/noesrafa/sunnytui/internal/clipboard"
	"github.com/noesrafa/sunnytui/internal/list"
	"github.com/noesrafa/sunnytui/internal/runs"
	"github.com/noesrafa/sunnytui/internal/session"
	"github.com/noesrafa/sunnytui/internal/state"
	"github.com/noesrafa/sunnytui/internal/sysstats"
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
	overlay       *Overlay
	initialCwd    string
	defaultModel  string
	defaultEffort string
	themeID       string // active theme; persisted in state.json
	skipPerms     bool
	lastErr       error
	lastCtrlC     time.Time

	// chat is the new list-based transcript viewport. It owns the full
	// drag-to-select-and-copy state machine (multi-click, backward drag,
	// auto-scroll past edges) so the parent Model just routes mouse + key
	// events through.
	chat *chatModel

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

	// logoAlive guards the logoTickCmd chain so we don't queue duplicates
	// when ensureLogoTick is called multiple times. The chain dies when
	// shouldAnimateLogo() goes false (sets this back to false) and is
	// resurrected by the next ensureLogoTick call that finds it idle.
	logoAlive bool

	// sysStats holds the most recent CPU + RAM sample. Refreshed by a
	// background tick (sysStatsTickCmd → sysStatsResultMsg).
	sysStats sysstats.Stats
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
	// Disable textarea's built-in ctrl+v paste — it only knows how to read
	// text from the clipboard (atotto.ReadAll) and silently drops image-only
	// clipboards. We intercept ctrl+v ourselves so we can do the image-aware
	// paste flow first, falling back to text when no image is present.
	ta.KeyMap.Paste = key.NewBinding(key.WithDisabled())
	ta.Focus()

	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	sp.Style = lipgloss.NewStyle().Foreground(colWarning)

	chatVP := newChatModel()
	chatVP.SetSize(80, 20)

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
		chat:          chatVP,
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
	// Note: the logo gradient sweep is intentionally NOT started here.
	// Sessions always restore as Idle (Thinking only flips on Send), so at
	// boot shouldAnimateLogo() is false and the logo stays frozen on its
	// last frame — still reads as a gradient, just static. The chain wakes
	// up via ensureLogoTick from handleSubmit when the user sends a turn.
	cmds := []tea.Cmd{textarea.Blink, branchTickCmd(), sysStatsSampleCmd(), sysStatsTickCmd()}
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
	stream := sess.Stream
	events := stream.Events()
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return sessionClosedMsg{SessionID: id, Stream: stream}
		}
		return sessionEventMsg{SessionID: id, Stream: stream, Event: ev}
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
			m.chat.ScrollToBottom()
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

// inChatRegion reports whether (x, y) in screen coords lies inside the chat
// list. Used to gate mouse events at the parent before forwarding into the
// chatModel — keeps drag-to-select from firing on the sidebar / textarea.
func (m Model) inChatRegion(x, y int) bool {
	if x < sidebarWidth+sidebarGap || x >= m.width {
		return false
	}
	if y < headerHeight {
		return false
	}
	if y >= headerHeight+m.chat.Height() {
		return false
	}
	return true
}

// screenToChat maps screen coords into the chat list's local coords (origin
// at the chat's top-left). Returns negative values when outside the chat,
// callers may clamp as needed for drag-past-edges behavior.
func (m Model) screenToChat(x, y int) (int, int) {
	return x - sidebarWidth - sidebarGap, y - headerHeight
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
	// Image paste: intercept BEFORE the textarea consumes the paste, so we
	// can swap binary clipboard content for an "[Image #N]" marker. If the
	// clipboard has no image, fall through and the textarea pastes text.
	pasteHandled := false
	if !m.overlay.HasOpen() {
		if pm, ok := msg.(tea.PasteMsg); ok {
			if m.tryImagePaste(pm.Content) {
				pasteHandled = true
			}
		}
	}
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
		// Drop close events from streams the session has already swapped out
		// (e.g. after Ctrl+R reset the claude process). Otherwise the old
		// goroutine would fire sessionClosedMsg when its channel drains and
		// we'd incorrectly tear down the freshly-restarted session.
		if sess := m.manager.ByID(msg.SessionID); sess == nil || (msg.Stream != nil && sess.Stream != msg.Stream) {
			break
		}
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
		if m.shouldAnimateLogo() {
			cmds = append(cmds, logoTickCmd())
		} else {
			m.logoAlive = false
		}
	case sysStatsTickMsg:
		// Tick fires the actual sample (off-thread); sample posts back via
		// sysStatsResultMsg. We re-arm the tick from the result handler so
		// a hung `top` doesn't queue overlapping samples.
		cmds = append(cmds, sysStatsSampleCmd())
	case sysStatsResultMsg:
		m.sysStats = msg.Stats
		cmds = append(cmds, sysStatsTickCmd())
	}

	if !m.overlay.HasOpen() && !pasteHandled {
		var cmd tea.Cmd
		prevValue := m.textarea.Value()
		// Snapshot whether the user was reading the latest message
		// BEFORE the textarea consumes the key. If they were pinned to
		// bottom and typing grows the textarea, we want to stay pinned
		// after layout shrinks the viewport; if they had scrolled up
		// to read history, we leave their position alone.
		wasAtBottom := m.chat.AtBottom()
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
		if m.textarea.Value() != prevValue {
			m.syncAttachmentMarkers() // drop attachments whose marker the user broke
			m.layout()                // dynamic textarea height
			if wasAtBottom {
				m.chat.ScrollToBottom()
			}
		}
		// Keyboard scroll: PgUp/PgDown forwards to the chat list. Other
		// keys are textarea territory.
		if km, ok := msg.(tea.KeyMsg); ok {
			switch km.String() {
			case "pgup":
				m.chat.PageUp()
			case "pgdown":
				m.chat.PageDown()
			}
		}
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
	case ConfirmCloseSessionMsg:
		m.overlay.CloseTop()
		m.handleCloseTab()
		return m, nil, true
	case ConfirmNewConvMsg:
		m.overlay.CloseTop()
		cur := m.manager.Current()
		if cur == nil {
			return m, nil, true
		}
		if err := cur.Reset(m.ctx, m.skipPerms); err != nil {
			cur.LastErr = err
			m.logger.Error("session reset failed", "session", cur.ID, "err", err)
			return m, nil, true
		}
		m.textarea.Reset()
		m.layout()
		m.refreshViewport()
		m.chat.ScrollToBottom()
		m.saveState()
		return m, waitForSession(cur), true
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
			m.chat.ScrollToBottom()
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

// updateMouse routes mouse events into the chatModel's drag-to-select state
// machine. Wheel scrolls; click/motion/release drive selection. Returns
// handled=true only for genuine MouseMsg values — other messages pass through.
func (m Model) updateMouse(msg tea.Msg) (Model, tea.Cmd, bool) {
	if mm, isWheel := msg.(tea.MouseWheelMsg); isWheel {
		// Horizontal wheel: drop. We don't support horizontal scroll in
		// the chat — the list does its own width-aware wrapping.
		if mm.Button == tea.MouseWheelLeft || mm.Button == tea.MouseWheelRight {
			return m, nil, true
		}
		if mm.Mod.Contains(tea.ModShift) {
			return m, nil, true
		}
		// Dialog open? Forward so its scrollable content can move.
		if m.overlay.HasOpen() {
			return m, m.overlay.UpdateTop(mm), true
		}
		// Pane mode: let the wheel pass to the embedded child terminal.
		if m.activeKind == activePane {
			return m, nil, false
		}
		// Vertical wheel on the chat: scroll a few lines per tick.
		step := 3
		if mm.Button == tea.MouseWheelUp {
			m.chat.ScrollBy(-step)
		} else if mm.Button == tea.MouseWheelDown {
			m.chat.ScrollBy(step)
		}
		return m, nil, true
	}
	mm, ok := msg.(tea.MouseMsg)
	if !ok {
		return m, nil, false
	}
	// Overlays and pane mode never get app-level drag-to-select.
	if m.overlay.HasOpen() || m.activeKind == activePane {
		return m, nil, true
	}
	e := mm.Mouse()
	cx, cy := m.screenToChat(e.X, e.Y)
	switch ev := mm.(type) {
	case tea.MouseClickMsg:
		if ev.Button != tea.MouseLeft {
			return m, nil, true
		}
		if !m.inChatRegion(e.X, e.Y) {
			m.chat.ClearMouse()
			return m, nil, true
		}
		_, cmd := m.chat.HandleMouseDown(cx, cy)
		return m, cmd, true
	case tea.MouseMotionMsg:
		// Drag past the visible area: chatModel clamps to first/last.
		m.chat.HandleMouseDrag(cx, cy)
		return m, nil, true
	case tea.MouseReleaseMsg:
		if m.chat.HandleMouseUp(cx, cy) {
			if m.chat.HasHighlight() {
				if text := m.chat.CopySelection(); text != "" {
					m.logger.Info("clipboard write", "len", len(text))
				}
			}
		}
		return m, nil, true
	}
	return m, nil, true
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
	case key.Matches(msg, m.keymap.Settings):
		return m, m.overlay.Open(NewSettingsDialog(m.themeID, m.styles)), true
	case key.Matches(msg, m.keymap.Game):
		return m, m.overlay.Open(NewMinigameDialog(m.styles)), true
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
	case key.Matches(msg, m.keymap.CloseSession):
		// Handled here (above the pane-forward block) so ctrl+w closes
		// the active terminal pane instead of getting eaten by the shell
		// inside it as a "delete word" keystroke. Panes close immediately;
		// claude sessions go through a confirmation dialog so the user
		// doesn't lose the transcript on a typo.
		if m.activeKind == activePane {
			m.handleCloseTab()
			return m, nil, true
		}
		if cur := m.manager.Current(); cur != nil {
			body := []string{"¿Cerrar la sesión \"" + cur.Title + "\"?"}
			if cur.State == session.StateThinking {
				body = append(body, "")
				body = append(body, "⚠ la sesión está pensando — el turno se cancelará")
			}
			d := NewConfirmDialog(m.styles, "Cerrar sesión", body, ConfirmCloseSessionMsg{})
			return m, m.overlay.Open(d), true
		}
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
	case key.Matches(msg, m.keymap.Diff):
		cwd, branch := "", ""
		var changes session.ChangeStats
		if cur := m.manager.Current(); cur != nil {
			cwd, branch, changes = cur.Cwd, cur.Branch, cur.Changes
		} else {
			cwd = m.initialCwd
		}
		return m, m.overlay.Open(NewDiffDialog(cwd, branch, changes, m.styles)), true
	case key.Matches(msg, m.keymap.NewConv):
		if cur := m.manager.Current(); cur != nil {
			body := []string{
				"¿Empezar una nueva conversación de claude?",
				"",
				"Se mantendrá la pestaña (" + cur.Title + ") en " + prettyPath(cur.Cwd) + ",",
				"pero el transcript actual se va a descartar.",
			}
			d := NewConfirmDialog(m.styles, "Nueva conversación", body, ConfirmNewConvMsg{})
			return m, m.overlay.Open(d), true
		}
		return m, nil, true
	case key.Matches(msg, m.keymap.Send):
		next, cmd := m.handleSend()
		return next, cmd, true
	case key.Matches(msg, m.keymap.Paste):
		m.handlePaste()
		return m, nil, true
	}
	return m, nil, false
}

// handlePaste runs the image-aware paste flow: image clipboard first
// (saves + drops "[Image #N]" marker), then falls back to plain text via
// atotto when there's no image. Triggered by Ctrl+V — terminals don't
// forward Cmd+V as bracketed paste when the clipboard is image-only.
func (m *Model) handlePaste() {
	if m.tryImagePaste("") {
		return
	}
	text, err := clipboard.ReadAll()
	if err != nil {
		m.logger.Debug("clipboard text read", "err", err)
		return
	}
	if text == "" {
		return
	}
	m.textarea.InsertString(text)
	if cur := m.manager.Current(); cur != nil {
		cur.Draft = m.textarea.Value()
	}
	m.layout()
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
		// Drop any pending pasted images too — their markers vanished with
		// the textarea reset, so keeping them would only let stale images
		// resolve into a future message by accident.
		cur.Attachments = nil
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

// syncAttachmentMarkers reconciles the textarea against pending
// attachments. If the user damaged a marker (e.g., backspaced over the
// closing bracket of "[Image #2]"), we drop that attachment from the
// pending list, scrub any orphan fragment from the textarea, and delete
// the file from disk — it has never been sent and will never be sent.
//
// Called after every textarea-mutating keystroke. Cheap: O(pending
// attachments × textarea length).
func (m *Model) syncAttachmentMarkers() {
	cur := m.manager.Current()
	if cur == nil || len(cur.Attachments) == 0 {
		return
	}
	text := m.textarea.Value()
	cleaned := text
	kept := cur.Attachments[:0]
	for _, a := range cur.Attachments {
		marker := fmt.Sprintf("[Image #%d]", a.Index)
		if strings.Contains(cleaned, marker) {
			kept = append(kept, a)
			continue
		}
		// Marker broken. Strip whatever fragment of it is still hanging
		// around so the textarea doesn't show "[Image #2" forever, then
		// delete the unused file.
		fragment := regexp.MustCompile(fmt.Sprintf(`\[?Image\s*#?\s*%d\b\]?`, a.Index))
		cleaned = fragment.ReplaceAllString(cleaned, "")
		if err := os.Remove(a.Path); err != nil && !os.IsNotExist(err) {
			m.logger.Debug("remove orphan image", "path", a.Path, "err", err)
		}
		m.logger.Info("attachment dropped",
			"session", cur.ID,
			"idx", a.Index,
			"path", a.Path,
		)
	}
	cur.Attachments = kept
	if cleaned != text {
		m.textarea.SetValue(cleaned)
		m.textarea.CursorEnd()
		cur.Draft = cleaned
	}
}

// tryImagePaste peeks at the system clipboard for image data. If found, it
// saves the bytes under ~/.sunnytui/images/, registers an attachment on
// the active session, and inserts the matching "[Image #N]" marker at the
// textarea cursor. Any text that came along in the bracketed paste is
// inserted right after the marker so users don't lose accompanying text.
//
// Returns true when the message has been fully handled (the caller MUST
// skip forwarding the original PasteMsg to the textarea, or the binary
// junk will land as garbled glyphs).
func (m *Model) tryImagePaste(text string) bool {
	cur := m.manager.Current()
	if cur == nil {
		return false
	}
	data, mediaType, ok, err := imgclip.ReadImage()
	if err != nil {
		m.logger.Debug("clipboard image read", "err", err)
	}
	if !ok {
		return false
	}
	path, err := imgclip.SaveImage(data, mediaType)
	if err != nil {
		m.logger.Warn("save attachment", "err", err)
		return false
	}
	idx := cur.AddAttachment(path, mediaType)
	marker := fmt.Sprintf("[Image #%d]", idx)
	insert := marker
	if text != "" {
		insert = marker + " " + text
	}
	m.textarea.InsertString(insert)
	cur.Draft = m.textarea.Value()
	m.layout()
	m.logger.Info("image pasted",
		"session", cur.ID,
		"idx", idx,
		"path", path,
		"bytes", len(data),
	)
	return true
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
	m.chat.ScrollToBottom()
	// Kick off the spinner tick chain, the morphing-string anim, and the
	// logo gradient sweep. Each fires its own re-arm message; they die when
	// the session goes back to idle.
	return m, tea.Batch(m.spinner.Tick, m.thinkingAnim.Step(), (&m).ensureLogoTick())
}

func (m *Model) handleSessionEvent(msg sessionEventMsg) tea.Cmd {
	sess := m.manager.ByID(msg.SessionID)
	if sess == nil {
		return nil
	}
	// Drop events from streams the session has already swapped out (Ctrl+R
	// reset). The new stream's waitForSession will keep the chain alive.
	if msg.Stream != nil && sess.Stream != msg.Stream {
		return nil
	}
	wasThinking := sess.State == session.StateThinking
	sess.HandleEvent(msg.Event)
	if cur := m.manager.Current(); cur != nil && cur.ID == sess.ID {
		m.refreshViewport()
		m.chat.ScrollToBottom()
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

// shouldAnimateLogo returns true while there is something "live" the user
// might want feedback on: a session generating a turn, a run still alive,
// or an open overlay (whose dialog could itself be animating). Outside
// those, the logo freezes on its last frame — same gradient, just static.
func (m *Model) shouldAnimateLogo() bool {
	return m.anyThinking() || m.anyRunRunning() || m.overlay.HasOpen()
}

// ensureLogoTick is the resurrection point for the logo animation chain.
// Idempotent: returns nil if a tick is already in flight or if there's
// nothing to animate. Call wherever the model transitions toward an
// active state (Send, overlay open, run start) so the logo wakes up.
func (m *Model) ensureLogoTick() tea.Cmd {
	if m.logoAlive || !m.shouldAnimateLogo() {
		return nil
	}
	m.logoAlive = true
	return logoTickCmd()
}

// logoTickCmd drives the brand-mark gradient sweep. 120ms cadence keeps
// the animation visible without saturating the program loop on idle
// terminals. Each tick increments Model.logoFrame and re-arms itself.
func logoTickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return logoTickMsg{} })
}

// sysStatsTickCmd is the metronome for CPU/RAM sampling — 4s cadence
// keeps the bars feeling live without making `top` a measurable share of
// our own CPU footprint. Tick → sample → tick is intentionally split
// across two messages so the actual `top` invocation runs off the main
// loop.
func sysStatsTickCmd() tea.Cmd {
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return sysStatsTickMsg{} })
}

func sysStatsSampleCmd() tea.Cmd {
	return func() tea.Msg {
		st, _ := sysstats.Sample()
		return sysStatsResultMsg{Stats: st}
	}
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
	m.chat.SetSize(mainW, vpH)

	taW := mainW - 4
	if taW < 10 {
		taW = 10
	}
	m.textarea.SetWidth(taW)
}

// refreshViewport rebuilds the chat list from the active session's items.
// The chat list owns its own render cache + selection overlay so this is
// idempotent and cheap when the items array hasn't changed.
func (m *Model) refreshViewport() {
	cur := m.manager.Current()
	if cur == nil {
		m.chat.SetItems(nil)
		return
	}
	if len(cur.Items) == 0 && cur.State == session.StateIdle {
		// Render the welcome screen as a single pseudo-item so the list
		// shows it. Wrapping a plain string in a stringItem keeps the
		// list machinery happy without a special-case render path.
		m.chat.SetItems([]list.Item{stringItem(m.welcomeText())})
		return
	}
	ctx := RenderContext{
		Width:     m.chat.Width(),
		Styles:    m.styles,
		LiveFrame: m.spinner.View(),
		Markdown:  m.markdown,
		ModelName: cur.Model,
	}
	if ctx.Width <= 0 {
		ctx.Width = m.width
	}
	items := buildChatItems(cur, ctx)
	if cur.State == session.StateThinking {
		label := cur.LiveStatus()
		if label == "" {
			label = "thinking"
		}
		m.thinkingAnim.SetLabel(label)
		items = append(items, stringItem("  "+m.thinkingAnim.Render()))
	}
	m.chat.SetItems(items)
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
		row("ctrl+r", "nueva conversación en la sesión actual"),
		row("ctrl+d", "ver el diff del repo"),
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
	w := m.chat.Width() - 4
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
	m.pruneOrphanImages()
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

// pruneOrphanImages walks every session's transcript, collects the
// image paths still referenced by past UserItems, and deletes any other
// file under ~/.sunnytui/images/. Pending (unsent) attachments aren't
// included because the app just started — there's nothing pending yet.
// Best-effort; failures only get logged.
func (m Model) pruneOrphanImages() {
	if m.manager == nil {
		return
	}
	refs := map[string]bool{}
	for _, s := range m.manager.Sessions {
		for _, it := range s.Items {
			u, ok := it.(session.UserItem)
			if !ok {
				continue
			}
			for _, a := range u.Attachments {
				refs[a.Path] = true
			}
		}
	}
	n, err := imgclip.PruneOrphans(refs)
	if err != nil {
		m.logger.Warn("prune images", "err", err)
		return
	}
	if n > 0 {
		m.logger.Info("pruned orphan images", "count", n)
	}
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
		eff := s.Effort
		if eff == "" {
			eff = m.defaultEffort
		}
		sessions = append(sessions, state.SavedSession{
			Title:     s.Title,
			Cwd:       s.Cwd,
			Model:     s.Model,
			Effort:    eff,
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
