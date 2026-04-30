// Package autoupdate runs `brew update && brew upgrade sunnytui` in a fully
// detached background process when the user launches sunny, so the next time
// they open it they're already on the latest tag.
//
// The current process never gets the new binary — replacing a running mach-o
// is fine on macOS but our long-lived TUI keeps using the old image until exit.
// That's the intended UX: zero startup latency, fresh version next launch.
//
// Guards:
//   - Only runs when the resolved binary lives under a Homebrew Cellar.
//     Source builds (`make install`, `go run`) are left alone.
//   - Throttled via ~/.sunnytui/last-update-check (default 6h).
//   - Process is detached with Setsid so it survives sunny's exit.
//   - Output is appended to ~/.sunnytui/autoupdate.log.
package autoupdate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"charm.land/log/v2"
)

const (
	throttle      = 6 * time.Hour
	timestampFile = "last-update-check"
	logFile       = "autoupdate.log"
)

// MaybeRunInBackground spawns the brew update if eligible and returns
// immediately. Never blocks, never errors out — failures are logged.
func MaybeRunInBackground(lg *log.Logger) {
	exe, err := resolvedExecutable()
	if err != nil {
		lg.Debug("autoupdate skip: cannot resolve executable", "err", err)
		return
	}
	if !installedViaBrew(exe) {
		lg.Debug("autoupdate skip: not a brew install", "exe", exe)
		return
	}
	if _, err := exec.LookPath("brew"); err != nil {
		lg.Debug("autoupdate skip: brew not on PATH", "err", err)
		return
	}
	dir, derr := stateDir()
	if derr != nil {
		lg.Debug("autoupdate skip: no home dir", "err", derr)
		return
	}
	if recentlyChecked(filepath.Join(dir, timestampFile)) {
		lg.Debug("autoupdate skip: throttled")
		return
	}
	// Touch the timestamp BEFORE spawning so a quick re-launch doesn't
	// kick off a second brew run.
	writeTimestamp(filepath.Join(dir, timestampFile))

	if err := spawn(filepath.Join(dir, logFile)); err != nil {
		lg.Warn("autoupdate spawn failed", "err", err)
		return
	}
	lg.Info("autoupdate kicked off in background", "log", filepath.Join(dir, logFile))
}

func resolvedExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		return real, nil
	}
	return exe, nil
}

// installedViaBrew returns true when the resolved path lives inside a
// Homebrew Cellar — the only place we trust `brew upgrade sunnytui` to do
// the right thing. Covers both Apple Silicon (/opt/homebrew) and Intel
// (/usr/local) layouts, plus user-relocated brew prefixes.
func installedViaBrew(exe string) bool {
	return strings.Contains(exe, "/Cellar/sunnytui/")
}

func stateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".sunnytui")
	_ = os.MkdirAll(dir, 0o755)
	return dir, nil
}

func recentlyChecked(path string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	secs, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return false
	}
	return time.Since(time.Unix(secs, 0)) < throttle
}

func writeTimestamp(path string) {
	_ = os.WriteFile(path, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0o644)
}

func spawn(logPath string) error {
	// Append-mode log so successive runs accumulate. Capped reasonably by
	// the user — brew output per run is small (KBs).
	out, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()

	header := "\n--- " + time.Now().Format(time.RFC3339) + " ---\n"
	_, _ = out.WriteString(header)

	// `brew update` refreshes formula metadata; `brew upgrade sunnytui`
	// installs the new tag if one exists. `|| true` so a no-op upgrade
	// (already current) doesn't taint the exit code in the log.
	cmd := exec.Command("/bin/sh", "-c", "brew update && brew upgrade sunnytui || true")
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Stdin = nil
	// Setsid detaches the child from sunny's controlling terminal and
	// process group, so when the user exits sunny the brew run keeps
	// going to completion.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
