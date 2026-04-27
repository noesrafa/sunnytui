package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"

	"github.com/noesrafa/sunnytui/internal/claude"
	"github.com/noesrafa/sunnytui/internal/logger"
	"github.com/noesrafa/sunnytui/internal/runs"
	"github.com/noesrafa/sunnytui/internal/session"
	"github.com/noesrafa/sunnytui/internal/state"
	"github.com/noesrafa/sunnytui/internal/terminal"
	"github.com/noesrafa/sunnytui/internal/tui"
	"github.com/noesrafa/sunnytui/internal/usage"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "chat":
		if err := runChat(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "statusline":
		if err := runStatusline(); err != nil {
			fmt.Fprintln(os.Stderr, "statusline error:", err)
			os.Exit(1)
		}
	case "statusline-install":
		if err := installStatusline(); err != nil {
			fmt.Fprintln(os.Stderr, "install error:", err)
			os.Exit(1)
		}
	case "spike":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "spike: missing prompt")
			os.Exit(2)
		}
		prompt := strings.Join(os.Args[2:], " ")
		if err := runSpike(prompt); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "stream-test":
		if err := runStreamTest(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "-h", "--help", "help":
		printUsage()
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", os.Args[1])
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `sunnytui — multi-session Claude Code TUI

Commands:
  chat [--cwd DIR] [--model NAME] [--effort LEVEL]
                            open the TUI. model: opus|sonnet|haiku.
                            effort: low|medium|high|xhigh|max.
  spike <prompt>            M1: run one Claude turn and pretty-print events
  stream-test <p1> <p2>...  M2 backend: send N prompts on one streaming session
  statusline                act as Claude Code's statusline (reads stdin JSON,
                            persists usage snapshot for the chat sidebar)
  statusline-install        print instructions to register sunnytui as the
                            Claude Code statusline command
  help                      show this message`)
}

// runStatusline is invoked by Claude Code as its configured statusLine.command.
// Claude Code pipes a JSON payload to our stdin (model, context_window,
// rate_limits, …) on each refresh; we persist it to disk so the chat sidebar
// can read percentages later. Whatever we print to stdout becomes the line
// Claude Code shows to the user.
func runStatusline() error {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	if len(raw) == 0 {
		fmt.Println("☀ sunnytui")
		return nil
	}
	if err := usage.Write(raw); err != nil {
		fmt.Fprintln(os.Stderr, "warn: persist snapshot:", err)
	}
	// Print a compact one-line summary so Claude Code has a statusline to show.
	if p, _, perr := usage.Read(0); perr == nil && p != nil && p.RateLimits != nil {
		var parts []string
		if w := p.RateLimits.FiveHour; w != nil {
			parts = append(parts, fmt.Sprintf("5h %d%%", w.UsedPercentage))
		}
		if w := p.RateLimits.SevenDay; w != nil {
			parts = append(parts, fmt.Sprintf("7d %d%%", w.UsedPercentage))
		}
		fmt.Println("☀ " + strings.Join(parts, " · "))
	} else {
		fmt.Println("☀ sunnytui")
	}
	return nil
}

func installStatusline() error {
	exe, err := exec.LookPath("sunnytui")
	if err != nil {
		exe, _ = os.Executable()
	}
	fmt.Printf(`Add this to ~/.claude/settings.json (creating "statusLine" if needed):

  "statusLine": {
    "type": "command",
    "command": "%s statusline"
  }

Then run `+"`claude`"+` interactively (any project, any prompt) once. Claude Code
will refresh its statusline and our subcommand will write the snapshot to:

  %s

After that, sunnytui chat's "usage" widget will show the percentages.
`, exe, usage.SnapshotPath())
	return nil
}

func runChat(args []string) error {
	cwd := ""
	model := "opus"
	effort := "max"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--cwd":
			if i+1 < len(args) {
				cwd = args[i+1]
				i++
			}
		case "--model":
			if i+1 < len(args) {
				model = args[i+1]
				i++
			}
		case "--effort":
			if i+1 < len(args) {
				effort = args[i+1]
				i++
			}
		}
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	lg, closer := logger.Setup("sunnytui")
	defer closer.Close()
	lg.Info("chat starting", "cwd", cwd, "model", model, "effort", effort, "log", logger.LogPath())

	mgr := session.NewManager()
	paneMgr := terminal.NewManager()

	// Single source of truth for between-runs state. v2: includes panes too.
	saved, _ := state.Load()
	if saved == nil {
		saved = &state.State{ActiveKind: "claude"}
	}

	for _, ss := range saved.Sessions {
		s, sErr := session.New(ctx, ss.Cwd, session.Options{
			Logger:                   lg,
			Model:                    cmp.Or(ss.Model, model),
			Effort:                   cmp.Or(ss.Effort, effort),
			DangerousSkipPermissions: true,
			ResumeID:                 ss.RemoteID,
			Title:                    ss.Title,
			Draft:                    ss.Draft,
		})
		if sErr != nil {
			lg.Warn("restore session failed", "cwd", ss.Cwd, "err", sErr)
			continue
		}
		mgr.Add(s)
	}
	if mgr.Len() == 0 {
		// Fresh install or all sessions failed to restore — open one default.
		first, err := session.New(ctx, cwd, session.Options{
			Logger:                   lg,
			Model:                    model,
			Effort:                   effort,
			DangerousSkipPermissions: true,
		})
		if err != nil {
			return fmt.Errorf("start initial session: %w", err)
		}
		mgr.Add(first)
	}

	for _, sp := range saved.Panes {
		p, err := terminal.Spawn(sp.Title, sp.Command, sp.Cwd, 80, 24)
		if err != nil {
			lg.Warn("respawn pane failed", "name", sp.Title, "cmd", sp.Command, "err", err)
			continue
		}
		paneMgr.Add(p)
	}
	defer paneMgr.CloseAll()

	// Apply the saved active-tab pointer.
	switch saved.ActiveKind {
	case "claude":
		if saved.ActiveIdx >= 0 && saved.ActiveIdx < mgr.Len() {
			mgr.Active = saved.ActiveIdx
		}
	case "pane":
		if saved.ActiveIdx >= 0 && saved.ActiveIdx < paneMgr.Len() {
			paneMgr.Active = saved.ActiveIdx
		}
	}

	runMgr, err := runs.Load()
	if err != nil {
		lg.Warn("runs.Load failed, starting empty", "err", err)
		runMgr = &runs.Manager{}
	}
	defer runMgr.StopAll()

	lg.Info("state restored",
		"sessions", mgr.Len(),
		"panes", paneMgr.Len(),
		"active_kind", saved.ActiveKind,
		"active_idx", saved.ActiveIdx,
	)

	root := tui.NewModel(ctx, mgr, cwd, tui.Options{
		Logger:                   lg,
		DefaultModel:             model,
		DefaultEffort:            effort,
		DangerousSkipPermissions: true,
		Runs:                     runMgr,
		Panes:                    paneMgr,
		InitialActiveKind:        saved.ActiveKind,
	})
	return root.Run(ctx)
}

func runStreamTest(prompts []string) error {
	if len(prompts) == 0 {
		return fmt.Errorf("stream-test: need at least one prompt")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cwd, _ := os.Getwd()
	stream, err := claude.NewStream(ctx, claude.StreamOpts{Cwd: cwd, Stderr: os.Stderr})
	if err != nil {
		return err
	}
	defer stream.Close()

	for i, p := range prompts {
		fmt.Printf("%s>> turn %d: %s%s\n", dim, i+1, p, reset)
		if err := stream.Send(p); err != nil {
			return err
		}
		for ev := range stream.Events() {
			printEvent(ev)
			if ev.Type == "result" {
				break
			}
		}
	}
	return nil
}

func runSpike(prompt string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cwd, _ := os.Getwd()
	events, err := claude.RunOnce(ctx, claude.RunOnceOpts{Cwd: cwd, Stderr: os.Stderr}, prompt)
	if err != nil {
		return err
	}
	for ev := range events {
		printEvent(ev)
	}
	return nil
}

const (
	dim    = "\033[2m"
	cyan   = "\033[36m"
	mag    = "\033[35m"
	green  = "\033[32m"
	yellow = "\033[33m"
	red    = "\033[31m"
	reset  = "\033[0m"
)

func printEvent(ev claude.Event) {
	switch ev.Type {
	case "system":
		if ev.Subtype == "init" {
			fmt.Printf("%s[init]%s session=%s model=%s cwd=%s\n",
				dim, reset, short(ev.SessionID), ev.Model, ev.Cwd)
		}
	case "assistant":
		if ev.Message == nil {
			return
		}
		for _, b := range ev.Message.Content {
			switch b.Type {
			case "text":
				fmt.Printf("%s[assistant]%s %s\n", cyan, reset, b.Text)
			case "tool_use":
				fmt.Printf("%s[tool_use]%s %s %s\n", mag, reset, b.Name, compact(b.Input, 80))
			default:
				fmt.Printf("%s[assistant:%s]%s\n", cyan, b.Type, reset)
			}
		}
	case "user":
		if ev.Message != nil {
			for _, b := range ev.Message.Content {
				if b.Type == "tool_result" {
					fmt.Printf("%s[tool_result]%s %s\n", yellow, reset, compact(b.Content, 80))
				}
			}
		}
	case "result":
		color := green
		tag := "ok"
		if ev.IsError {
			color, tag = red, "error"
		}
		fmt.Printf("%s[result]%s %s in %dms cost=$%.4f turns=%d\n",
			color, reset, tag, ev.DurationMs, ev.TotalCostUSD, ev.NumTurns)
	case "rate_limit_event":
		// quiet
	case "parse_error":
		fmt.Printf("%s[parse_error]%s %s\n", red, reset, ev.Result)
	default:
		fmt.Printf("%s[%s]%s\n", dim, ev.Type, reset)
	}
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func compact(raw json.RawMessage, max int) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
