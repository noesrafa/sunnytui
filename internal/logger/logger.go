package logger

import (
	"io"
	"os"
	"path/filepath"
	"time"

	"charm.land/log/v2"
)

// Setup opens (or creates) ~/.sunnytui/sunnytui.log for append and returns a
// configured logger plus an io.Closer for the underlying file. Failures fall
// back to a logger writing to io.Discard so callers can keep going.
func Setup(prefix string) (*log.Logger, io.Closer) {
	home, err := os.UserHomeDir()
	if err != nil {
		return discard(prefix), noopCloser{}
	}
	dir := filepath.Join(home, ".sunnytui")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return discard(prefix), noopCloser{}
	}
	f, err := os.OpenFile(filepath.Join(dir, "sunnytui.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return discard(prefix), noopCloser{}
	}
	logger := log.NewWithOptions(f, log.Options{
		ReportTimestamp: true,
		TimeFormat:      time.RFC3339Nano,
		Level:           log.DebugLevel,
		Prefix:          prefix,
	})
	logger.Info("logger started", "pid", os.Getpid())
	return logger, f
}

func LogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".sunnytui", "sunnytui.log")
}

func discard(prefix string) *log.Logger {
	return log.NewWithOptions(io.Discard, log.Options{Prefix: prefix})
}

type noopCloser struct{}

func (noopCloser) Close() error { return nil }
