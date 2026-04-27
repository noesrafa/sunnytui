package highlight

import (
	"strings"
	"testing"
)

func TestExtract_SingleLine(t *testing.T) {
	got := Extract("hello world", 20, 1, 0, 6, 0, 11)
	if got != "world" {
		t.Fatalf("got %q, want %q", got, "world")
	}
}

func TestExtract_MultiLine(t *testing.T) {
	content := "line one\nline two\nline three"
	// Select from "one" through "two": (0, 5)..(1, 8)
	got := Extract(content, 20, 3, 0, 5, 1, 8)
	want := "one\nline two"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExtract_EmptyOnZeroRange(t *testing.T) {
	got := Extract("hello", 20, 1, 0, 0, 0, 0)
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestApply_LeavesContentShape(t *testing.T) {
	content := "line one\nline two\nline three"
	got := Apply(content, 20, 3, 0, 0, 1, 8)
	// Same number of lines; reverse-video escapes inserted somewhere.
	if strings.Count(got, "\n") < strings.Count(content, "\n") {
		t.Fatalf("apply shrunk line count: got %d lines, want at least %d", strings.Count(got, "\n"), strings.Count(content, "\n"))
	}
	if !strings.Contains(got, "\x1b[7m") {
		t.Fatalf("expected reverse-video escape in output, got %q", got)
	}
}
