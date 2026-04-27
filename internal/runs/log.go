package runs

import "sync"

// LogBuffer is a ring buffer of recent log lines for a single Run. Safe for
// concurrent appends from the stdout/stderr capture goroutines while the UI
// reads via Snapshot.
type LogBuffer struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func NewLogBuffer(max int) *LogBuffer {
	if max <= 0 {
		max = 500
	}
	return &LogBuffer{max: max, lines: make([]string, 0, max)}
}

func (b *LogBuffer) Append(line string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines = append(b.lines, line)
	if len(b.lines) > b.max {
		drop := len(b.lines) - b.max
		b.lines = b.lines[drop:]
	}
}

// Snapshot returns a copy of the current lines, safe for the caller to use
// without holding the lock.
func (b *LogBuffer) Snapshot() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.lines))
	copy(out, b.lines)
	return out
}

func (b *LogBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines = b.lines[:0]
}

func (b *LogBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.lines)
}
