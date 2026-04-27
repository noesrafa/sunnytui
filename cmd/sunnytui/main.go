package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/noesrafa/sunnytui/internal/claude"
	"github.com/noesrafa/sunnytui/internal/logger"
	"github.com/noesrafa/sunnytui/internal/session"
	"github.com/noesrafa/sunnytui/internal/tui"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "chat":
		if err := runChat(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
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
		usage()
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `sunnytui — multi-session Claude Code TUI

Commands:
  chat [--cwd DIR] [--model NAME] [--effort LEVEL]
                            open the TUI. model: opus|sonnet|haiku.
                            effort: low|medium|high|xhigh|max.
  spike <prompt>            M1: run one Claude turn and pretty-print events
  stream-test <p1> <p2>...  M2 backend: send N prompts on one streaming session
  help                      show this message`)
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

	root := tui.NewModel(ctx, mgr, cwd, tui.Options{
		Logger:                   lg,
		DefaultModel:             model,
		DefaultEffort:            effort,
		DangerousSkipPermissions: true,
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
