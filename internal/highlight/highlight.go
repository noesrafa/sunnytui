// Package highlight applies a reverse-video selection overlay over already-
// rendered ANSI content, and extracts the plain text inside that selection
// for clipboard copy. Lifted almost verbatim from Crush's
// internal/ui/list/highlight.go — same uv.ScreenBuffer + uv.NewStyledString
// approach, just without the list.Item plumbing.
package highlight

import (
	"image"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// Apply renders content with a reverse-video overlay between
// (startLine, startCol) and (endLine, endCol). Coordinates are in CONTENT
// space (the rendered string's own line/column grid, before viewport
// cropping). Lines and columns are 0-indexed; endCol is exclusive in
// practice (we highlight up to but not including endCol).
//
// Returns a new ANSI string the same shape as content (same line count,
// same widths) — drop-in replacement for SetContent on a viewport.
func Apply(content string, width, height, startLine, startCol, endLine, endCol int) string {
	if width <= 0 || height <= 0 {
		return content
	}
	area := image.Rect(0, 0, width, height)
	buf := highlightBuffer(content, area, startLine, startCol, endLine, endCol, defaultHighlighter)
	if buf == nil {
		return content
	}
	return buf.Render()
}

// Extract returns the plain (un-styled) text inside the selection range,
// suitable for clipboard. Mirrors Crush's HighlightContent: walks cells in
// range and concatenates their content, inserting newlines between rows.
func Extract(content string, width, height, startLine, startCol, endLine, endCol int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	area := image.Rect(0, 0, width, height)
	var sb strings.Builder
	pos := image.Pt(-1, -1)
	highlightBuffer(content, area, startLine, startCol, endLine, endCol, func(x, y int, c *uv.Cell) *uv.Cell {
		if c == nil {
			return c
		}
		pos.X = x
		if pos.Y == -1 {
			pos.Y = y
		} else if y > pos.Y {
			sb.WriteString(strings.Repeat("\n", y-pos.Y))
			pos.Y = y
		}
		sb.WriteString(c.Content)
		return c
	})
	return strings.TrimRight(sb.String(), " \n")
}

type highlighter func(x, y int, c *uv.Cell) *uv.Cell

var defaultHighlighter highlighter = func(_, _ int, c *uv.Cell) *uv.Cell {
	if c == nil {
		return c
	}
	c.Style.Attrs |= uv.AttrReverse
	return c
}

// highlightBuffer is Crush's HighlightBuffer, copied with minor edits:
// dropped the stringext.NormalizeSpace call (sunnytui content already has
// no \r and we want to preserve leading whitespace inside the transcript).
func highlightBuffer(content string, area image.Rectangle, startLine, startCol, endLine, endCol int, h highlighter) *uv.ScreenBuffer {
	if startLine < 0 || startCol < 0 {
		return nil
	}
	if h == nil {
		h = defaultHighlighter
	}
	width, height := area.Dx(), area.Dy()
	buf := uv.NewScreenBuffer(width, height)
	styled := uv.NewStyledString(content)
	styled.Draw(&buf, area)

	// Treat -1 as "end of content".
	if endLine < 0 {
		endLine = height - 1
	}
	if endCol < 0 {
		endCol = width
	}

	for y := startLine; y <= endLine && y < height; y++ {
		if y >= buf.Height() {
			break
		}
		line := buf.Line(y)

		colStart := 0
		if y == startLine {
			colStart = min(startCol, len(line))
		}
		colEnd := len(line)
		if y == endLine {
			colEnd = min(endCol, len(line))
		}

		// Don't highlight trailing blank cells.
		lastContentX := -1
		for x := colStart; x < colEnd; x++ {
			cell := line.At(x)
			if cell == nil {
				continue
			}
			if cell.Content != "" && cell.Content != " " {
				lastContentX = x
			}
		}
		highlightEnd := colEnd
		if lastContentX >= 0 {
			highlightEnd = lastContentX + 1
		} else {
			highlightEnd = colStart
		}

		for x := colStart; x < highlightEnd; x++ {
			if !image.Pt(x, y).In(area) {
				continue
			}
			cell := line.At(x)
			if cell != nil {
				h(x, y, cell)
			}
		}
	}
	return &buf
}
