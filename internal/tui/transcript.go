package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/noesrafa/sunnytui/internal/session"
)

// RenderContext carries cross-cutting concerns into per-item rendering.
type RenderContext struct {
	Width     int
	Styles    Styles
	LiveFrame string             // current spinner frame for live tools
	Markdown  func(string) string // optional markdown renderer; nil → plain wrap
}

func RenderItem(it session.Item, ctx RenderContext) string {
	s := ctx.Styles
	switch v := it.(type) {
	case session.UserItem:
		prompt := s.UserPrompt.Render("›")
		body := s.UserText.Render(wrap(v.Text, ctx.Width-2))
		return lipgloss.JoinHorizontal(lipgloss.Top, prompt+" ", body)

	case session.AssistantTextItem:
		prompt := s.AssistantPrompt.Render("☀")
		var body string
		if ctx.Markdown != nil {
			body = ctx.Markdown(v.Text)
		} else {
			body = s.AssistantText.Render(wrap(v.Text, ctx.Width-2))
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, prompt+" ", body)

	case session.ThinkingItem:
		prompt := s.AssistantThink.Render("·")
		body := s.AssistantThink.Render(wrap(v.Text, ctx.Width-2))
		return lipgloss.JoinHorizontal(lipgloss.Top, prompt+" ", body)

	case session.ToolUseItem:
		return renderToolUse(v, ctx)

	case session.ToolResultItem:
		prompt := s.ToolPrompt.Render("↳")
		preview := truncateLines(v.Content, 6, ctx.Width-2)
		return lipgloss.JoinHorizontal(lipgloss.Top, prompt+" ", s.ToolResult.Render(preview))

	case session.EmptyResponseItem:
		prompt := s.AssistantPrompt.Render("☀")
		body := s.Hint.Render("(sin respuesta)")
		return lipgloss.JoinHorizontal(lipgloss.Top, prompt+" ", body)

	case session.ErrorItem:
		return s.ResultError.Render("✗ " + v.Message)

	case session.ResultItem:
		icon := s.ResultOK.Render("✓")
		if v.IsError {
			icon = s.ResultError.Render("✗")
		}
		meta := fmt.Sprintf("%.2fs", float64(v.DurationMs)/1000.0)
		return icon + " " + s.ResultMeta.Render(meta)
	}
	return ""
}

func renderToolUse(v session.ToolUseItem, ctx RenderContext) string {
	s := ctx.Styles
	var icon string
	switch {
	case !v.Done:
		frame := ctx.LiveFrame
		if frame == "" {
			frame = "◐"
		}
		icon = s.StatusBusy.Render(frame)
	case v.IsError:
		icon = s.ResultError.Render("✗")
	default:
		icon = s.ResultOK.Render("✓")
	}
	gear := s.ToolPrompt.Render("⚙")
	name := s.ToolName.Render(v.Name)
	header := fmt.Sprintf("%s %s %s", icon, gear, name)
	if len(v.Input) > 0 {
		inputBudget := ctx.Width - lipgloss.Width(header) - 2
		if inputBudget > 8 {
			header += " " + s.ToolInput.Render(compactJSON(v.Input, inputBudget))
		}
	}
	if !v.Done {
		return header
	}
	if v.Result == "" {
		return header
	}
	body := truncateLines(v.Result, 8, ctx.Width-4)
	indent := s.ToolPrompt.Render("  ↳ ")
	bodyStyle := s.ToolResult
	if v.IsError {
		bodyStyle = s.ResultError
	}
	return header + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, indent, bodyStyle.Render(body))
}

func RenderTranscript(items []session.Item, ctx RenderContext) string {
	if len(items) == 0 {
		return ctx.Styles.Hint.Render("escribe un mensaje y dale enter…")
	}
	parts := make([]string, 0, len(items)*2)
	for i, it := range items {
		parts = append(parts, RenderItem(it, ctx))
		if i < len(items)-1 {
			parts = append(parts, "")
		}
	}
	return strings.Join(parts, "\n")
}

func wrap(text string, width int) string {
	if width <= 0 {
		return text
	}
	var out strings.Builder
	for _, line := range strings.Split(text, "\n") {
		out.WriteString(wordwrap(line, width))
		out.WriteString("\n")
	}
	return strings.TrimRight(out.String(), "\n")
}

func wordwrap(line string, width int) string {
	if lipgloss.Width(line) <= width {
		return line
	}
	words := strings.Fields(line)
	var b strings.Builder
	col := 0
	for i, w := range words {
		wl := lipgloss.Width(w)
		if i == 0 {
			b.WriteString(w)
			col = wl
			continue
		}
		if col+1+wl > width {
			b.WriteByte('\n')
			b.WriteString(w)
			col = wl
		} else {
			b.WriteByte(' ')
			b.WriteString(w)
			col += 1 + wl
		}
	}
	return b.String()
}

func compactJSON(raw json.RawMessage, max int) string {
	s := strings.TrimSpace(string(raw))
	s = strings.ReplaceAll(s, "\n", " ")
	if max > 0 && lipgloss.Width(s) > max {
		s = s[:max] + "…"
	}
	return s
}

func truncateLines(text string, maxLines, width int) string {
	lines := strings.Split(text, "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines = append(lines, "…")
	}
	for i, l := range lines {
		if width > 0 && lipgloss.Width(l) > width {
			lines[i] = l[:width-1] + "…"
		}
	}
	return strings.Join(lines, "\n")
}
