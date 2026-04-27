package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

type StreamOpts struct {
	Cwd                      string
	SessionID                string // if set, --resume <id>
	Stderr                   io.Writer
	Model                    string // empty = claude default; alias like "opus" or full id
	Effort                   string // empty = default; one of: low, medium, high, xhigh, max
	DangerousSkipPermissions bool   // pass --dangerously-skip-permissions
}

// Stream is a long-lived `claude` process driven by line-delimited JSON on
// both stdin and stdout. Send a user message with Send(); read decoded
// events from Events(). The events channel closes when the process exits.
type Stream struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	events <-chan Event
	mu     sync.Mutex
	closed bool
}

func NewStream(ctx context.Context, opts StreamOpts) (*Stream, error) {
	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.Effort != "" {
		args = append(args, "--effort", opts.Effort)
	}
	if opts.DangerousSkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}
	if opts.SessionID != "" {
		args = append(args, "--resume", opts.SessionID)
	}
	cmd := exec.CommandContext(ctx, "claude", args...)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Stderr = opts.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start claude: %w", err)
	}

	decoded := Decode(stdout)
	out := make(chan Event, 64)
	go func() {
		defer close(out)
		for ev := range decoded {
			out <- ev
		}
		_ = cmd.Wait()
	}()

	return &Stream{cmd: cmd, stdin: stdin, events: out}, nil
}

// Send dispatches a user turn with a single text block. Convenience for
// callers that don't need image / multi-block input.
func (s *Stream) Send(text string) error {
	return s.SendBlocks([]map[string]any{{"type": "text", "text": text}})
}

// SendBlocks dispatches a user turn with arbitrary content blocks (text +
// image, in order). Each call triggers exactly one assistant turn.
func (s *Stream) SendBlocks(blocks []map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("stream closed")
	}
	payload := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": blocks,
		},
	}
	line, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	_, err = s.stdin.Write(line)
	return err
}

func (s *Stream) Events() <-chan Event { return s.events }

// Close shuts stdin (which the process treats as end-of-input) and waits
// for it to exit. The events channel will close shortly after.
func (s *Stream) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	_ = s.stdin.Close()
	return nil
}

// Cancel sends SIGINT to the underlying claude process to interrupt the
// current turn. The process may either abort the turn cleanly (emitting a
// result event with is_error=true) or exit; in the latter case the events
// channel closes and the session is cleaned up.
func (s *Stream) Cancel() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	return s.cmd.Process.Signal(os.Interrupt)
}
