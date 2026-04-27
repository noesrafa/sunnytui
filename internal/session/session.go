package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"charm.land/log/v2"

	"github.com/noesrafa/sunnytui/internal/claude"
)

// gitBranch returns the current branch of the given directory, or "" if it's
// not a git repo or git is unavailable. Uses --show-current which handles
// fresh repos with no commits gracefully (rev-parse fails on those).
func gitBranch(cwd string) string {
	out, err := exec.Command("git", "-C", cwd, "branch", "--show-current").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

type State int

const (
	StateIdle State = iota
	StateThinking
	StateError
)

func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateThinking:
		return "thinking"
	case StateError:
		return "error"
	}
	return "?"
}

type Session struct {
	ID        string // local UI id
	RemoteID  string // claude session_id (set after first init)
	Cwd       string
	Title     string
	Model     string
	Branch    string // git branch of Cwd (cached at creation)
	State     State
	Items     []Item
	TotalCost float64
	Turns     int
	LastErr   error
	StartedAt time.Time

	// RateLimit captures the last rate_limit_event from claude. Shared across
	// the user's account, but we store on the most-recently-active session.
	RateLimit *claude.RateLimitInfo

	// Draft is the current unsent textarea content. Saved when the user
	// switches sessions and restored when they switch back.
	Draft string

	Stream *claude.Stream

	logger        *log.Logger
	turnHadOutput bool
}

var idCounter atomic.Int64

func newID() string {
	n := idCounter.Add(1)
	return fmt.Sprintf("s%d", n)
}

type Options struct {
	Logger                   *log.Logger
	Model                    string
	Effort                   string
	DangerousSkipPermissions bool
	ResumeID                 string // claude session_id to --resume on startup
	Title                    string // override default basename(cwd) title (used by state restore)
	Draft                    string // pre-populate the textarea draft (state restore)
}

func New(ctx context.Context, cwd string, opts Options) (*Session, error) {
	if cwd == "" {
		return nil, fmt.Errorf("session: cwd required")
	}
	stream, err := claude.NewStream(ctx, claude.StreamOpts{
		Cwd:                      cwd,
		Model:                    opts.Model,
		Effort:                   opts.Effort,
		DangerousSkipPermissions: opts.DangerousSkipPermissions,
		SessionID:                opts.ResumeID,
	})
	if err != nil {
		return nil, err
	}
	id := newID()
	logger := opts.Logger
	if logger != nil {
		logger = logger.With("session", id, "cwd", cwd)
		logger.Info("session created", "model", opts.Model, "effort", opts.Effort)
	}
	title := opts.Title
	if title == "" {
		title = filepath.Base(cwd)
	}
	return &Session{
		ID:       id,
		Cwd:      cwd,
		Title:    title,
		Branch:   gitBranch(cwd),
		RemoteID: opts.ResumeID, // optimistic; will be confirmed when init event arrives
		Draft:    opts.Draft,
		Stream:   stream,
		State:    StateIdle,
		logger:   logger,
	}, nil
}

// Cancel interrupts the current turn (SIGINT to the claude subprocess).
func (s *Session) Cancel() error {
	if s.Stream == nil {
		return nil
	}
	if s.logger != nil {
		s.logger.Info("session cancel requested")
	}
	return s.Stream.Cancel()
}

// Send dispatches a user turn. Caller must check State == StateIdle.
func (s *Session) Send(text string) error {
	if s.State == StateThinking {
		return fmt.Errorf("session busy")
	}
	if s.logger != nil {
		s.logger.Debug("send", "len", len(text))
	}
	s.Items = append(s.Items, UserItem{Text: text})
	s.State = StateThinking
	s.StartedAt = time.Now()
	s.turnHadOutput = false
	if err := s.Stream.Send(text); err != nil {
		s.State = StateError
		s.LastErr = err
		if s.logger != nil {
			s.logger.Error("send failed", "err", err)
		}
		return err
	}
	return nil
}

// HandleEvent ingests one decoded claude event and updates transcript + state.
func (s *Session) HandleEvent(ev claude.Event) {
	if s.logger != nil {
		s.logger.Debug("event", "type", ev.Type, "subtype", ev.Subtype)
	}
	switch ev.Type {
	case "rate_limit_event":
		if ev.RateLimitInfo != nil {
			s.RateLimit = ev.RateLimitInfo
		}
	case "system":
		if ev.Subtype == "init" && s.RemoteID == "" {
			s.RemoteID = ev.SessionID
			s.Model = ev.Model
			if s.logger != nil {
				s.logger.Info("session init", "remote", ev.SessionID, "model", ev.Model)
			}
		}
	case "assistant":
		if ev.Message == nil {
			return
		}
		for _, b := range ev.Message.Content {
			switch b.Type {
			case "text":
				if strings.TrimSpace(b.Text) != "" {
					s.Items = append(s.Items, AssistantTextItem{Text: b.Text})
					s.turnHadOutput = true
				}
			case "thinking":
				if strings.TrimSpace(b.Text) != "" {
					s.Items = append(s.Items, ThinkingItem{Text: b.Text})
				}
			case "tool_use":
				s.Items = append(s.Items, ToolUseItem{ID: b.ID, Name: b.Name, Input: b.Input})
				s.turnHadOutput = true
				if s.logger != nil {
					s.logger.Info("tool_use", "name", b.Name, "id", b.ID)
				}
			}
		}
	case "user":
		if ev.Message == nil {
			return
		}
		for _, b := range ev.Message.Content {
			if b.Type == "tool_result" {
				if !s.linkToolResult(b.ToolUseID, b.Content, b.IsError) {
					s.Items = append(s.Items, ToolResultItem{Content: extractToolResult(b.Content)})
				}
			}
		}
	case "result":
		if !s.turnHadOutput {
			s.Items = append(s.Items, EmptyResponseItem{})
		}
		s.Items = append(s.Items, ResultItem{
			IsError:    ev.IsError,
			DurationMs: ev.DurationMs,
			CostUSD:    ev.TotalCostUSD,
			NumTurns:   ev.NumTurns,
		})
		s.TotalCost += ev.TotalCostUSD
		s.Turns++
		s.State = StateIdle
		if s.logger != nil {
			s.logger.Info("turn complete",
				"duration_ms", ev.DurationMs,
				"cost_usd", ev.TotalCostUSD,
				"is_error", ev.IsError,
			)
		}
	}
}

// linkToolResult finds the most recent ToolUseItem with the given id and
// attaches the result inline. Returns false if no match was found.
func (s *Session) linkToolResult(id string, content json.RawMessage, isError bool) bool {
	if id == "" {
		return false
	}
	for i := len(s.Items) - 1; i >= 0; i-- {
		tu, ok := s.Items[i].(ToolUseItem)
		if !ok {
			continue
		}
		if tu.ID != id {
			continue
		}
		tu.Done = true
		tu.IsError = isError
		tu.Result = extractToolResult(content)
		s.Items[i] = tu
		if s.logger != nil {
			s.logger.Info("tool_result", "name", tu.Name, "id", id, "is_error", isError, "result_len", len(tu.Result))
		}
		return true
	}
	return false
}

// LiveStatus returns a short verb describing what the session is doing right now.
// Empty string when idle.
func (s *Session) LiveStatus() string {
	if s.State != StateThinking {
		return ""
	}
	if len(s.Items) == 0 {
		return "thinking"
	}
	switch v := s.Items[len(s.Items)-1].(type) {
	case UserItem, ThinkingItem, ToolResultItem:
		return "thinking"
	case AssistantTextItem:
		return "writing"
	case ToolUseItem:
		if !v.Done {
			return "running " + v.Name
		}
		return "thinking"
	}
	return "thinking"
}

func (s *Session) Close() error {
	if s.Stream == nil {
		return nil
	}
	if s.logger != nil {
		s.logger.Info("session closing")
	}
	return s.Stream.Close()
}

func extractToolResult(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return str
	}
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err == nil {
		var parts []string
		for _, b := range arr {
			if t, ok := b["text"].(string); ok {
				parts = append(parts, t)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return string(raw)
}
