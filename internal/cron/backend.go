package cron

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"termux-monitor/internal/store"
)

// Run timings, shared with the OpenLocalPanel reference implementation.
const (
	// RunTimeout bounds a manual Run Now. Past the deadline the process group
	// is killed and the run is recorded as timed out.
	RunTimeout = 60 * time.Second
	// RunOutputCap keeps the tail of combined stdout+stderr per run.
	RunOutputCap = 8192
	// ProbeTimeout bounds short liveness probes (command -v, pgrep).
	ProbeTimeout = 8 * time.Second
)

// RunResult is one captured execution.
type RunResult struct {
	ExitCode  *int
	TimedOut  bool
	Output    string
	LatencyMs int64
}

// Backend is the crontab/crond surface the handlers depend on. Keeping it an
// interface lets tests drive the routes without a real cron installation.
type Backend interface {
	// ReadLines returns `crontab -l` split by newline; an absent crontab
	// resolves to an empty slice rather than an error.
	ReadLines() []string
	// WriteLines replaces the whole crontab.
	WriteLines(lines []string) error
	// HasBin reports whether a command is resolvable in $PREFIX.
	HasBin(bin string) bool
	// RunCommand executes a command with capture, honouring RunTimeout.
	RunCommand(command string) RunResult
	// DaemonPID returns the crond PID, or 0 when it is not running.
	DaemonPID() int
	// SpawnDetached launches crond detached from the server lifecycle.
	SpawnDetached() error
	// StopDaemon terminates a crond started from this panel.
	StopDaemon() error
	// Available reports whether Termux is usable at all.
	Available() (bool, string)
}

// TermuxBackend runs every command as the Termux application user, because the
// server itself is root inside a Magisk module and crontab/crond must own the
// Termux user's spool directory.
type TermuxBackend struct{}

// NewBackend returns the production crontab backend.
func NewBackend() *TermuxBackend { return &TermuxBackend{} }

// termuxCommand builds a command running script as the Termux user.
func termuxCommand(ctx context.Context, script string) (*exec.Cmd, error) {
	return store.TermuxCommand(ctx, script)
}

// run executes script through the Termux shell and returns its combined output.
func run(ctx context.Context, script string) (string, error) {
	cmd, err := termuxCommand(ctx, script)
	if err != nil {
		return "", err
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Available reports whether the Termux environment is usable.
func (TermuxBackend) Available() (bool, string) {
	reason := store.AvailableReason()
	return reason == "", reason
}

// ReadLines returns the Termux user's crontab split by newline. An absent
// crontab (non-zero exit) resolves to an empty slice: from the panel's point of
// view no panel job exists yet.
func (TermuxBackend) ReadLines() []string {
	ctx, cancel := context.WithTimeout(context.Background(), ProbeTimeout)
	defer cancel()
	out, err := run(ctx, "crontab -l 2>/dev/null")
	if err != nil && strings.TrimSpace(out) == "" {
		return nil
	}
	if out == "" {
		return nil
	}
	text := strings.ReplaceAll(out, "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	return strings.Split(text, "\n")
}

// WriteLines replaces the whole crontab. The content is fed on stdin so a large
// crontab never hits the command-length limit of a shell argument.
func (TermuxBackend) WriteLines(lines []string) error {
	script := "crontab -"
	cmd, err := termuxCommand(context.Background(), script)
	if err != nil {
		return err
	}
	body := ""
	if len(lines) > 0 {
		body = strings.Join(lines, "\n") + "\n"
	}
	cmd.Stdin = strings.NewReader(body)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return errors.New("crontab failed: " + truncateTail(msg, 300))
		}
		return err
	}
	return nil
}

// HasBin reports whether bin resolves inside the Termux $PREFIX.
func (TermuxBackend) HasBin(bin string) bool {
	if !isSafeToken(bin) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), ProbeTimeout)
	defer cancel()
	_, err := run(ctx, "command -v "+bin+" >/dev/null 2>&1")
	return err == nil
}

// RunCommand executes a command as the Termux user and captures the tail of its
// combined output. A timeout (or a kill by signal) yields TimedOut with a nil
// exit code, matching the OLP contract.
func (TermuxBackend) RunCommand(command string) RunResult {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), RunTimeout)
	defer cancel()

	// setsid puts the job in its own process group so a timeout can reap the
	// whole tree; the wrapper also honours the deadline by exiting 124.
	script := "setsid sh -c " + quoteSingle(command)
	cmd, err := termuxCommand(ctx, script)
	if err != nil {
		return RunResult{LatencyMs: time.Since(start).Milliseconds(), Output: err.Error()}
	}
	out, err := cmd.CombinedOutput()
	latency := time.Since(start).Milliseconds()
	capped := truncateTail(string(out), RunOutputCap)
	if ctx.Err() == context.DeadlineExceeded {
		return RunResult{TimedOut: true, Output: capped, LatencyMs: latency}
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code := ee.ExitCode()
			return RunResult{ExitCode: &code, Output: capped, LatencyMs: latency}
		}
		return RunResult{Output: capped, LatencyMs: latency}
	}
	zero := 0
	return RunResult{ExitCode: &zero, Output: capped, LatencyMs: latency}
}

// SpawnDetached starts crond in its own session so it survives the server
// process. The PID file lets StopDaemon kill exactly the daemon this panel
// started instead of every crond on the device.
func (TermuxBackend) SpawnDetached() error {
	script := "setsid nohup crond -b >/dev/null 2>&1 & echo $!"
	ctx, cancel := context.WithTimeout(context.Background(), ProbeTimeout)
	defer cancel()
	out, err := run(ctx, script)
	if err != nil {
		return err
	}
	pid := firstInt(string(out))
	if pid > 0 {
		_ = store.WriteStateFile("crond.pid", strconv.Itoa(pid))
	}
	return nil
}

// DaemonPID returns the crond PID, preferring the PID file and falling back to
// a process lookup for a daemon started outside the panel (boot scripts).
func (TermuxBackend) DaemonPID() int {
	ctx, cancel := context.WithTimeout(context.Background(), ProbeTimeout)
	defer cancel()
	if out, err := run(ctx, "cat "+store.TermuxRuntime("crond.pid")+" 2>/dev/null"); err == nil {
		if pid := firstInt(out); pid > 0 {
			if _, err := run(ctx, "kill -0 "+strconv.Itoa(pid)+" 2>/dev/null"); err == nil {
				return pid
			}
		}
	}
	// Fallback: `pidof crond` finds a daemon started by the module boot script.
	if out, err := run(ctx, "pidof crond 2>/dev/null"); err == nil {
		if pid := firstInt(out); pid > 0 {
			return pid
		}
	}
	return 0
}

// StopDaemon terminates the crond recorded in the PID file. Without a PID file
// only a daemon matched by pidof is stopped, and only when it is a crond.
func (TermuxBackend) StopDaemon() error {
	ctx, cancel := context.WithTimeout(context.Background(), ProbeTimeout)
	defer cancel()
	pidPath := store.TermuxRuntime("crond.pid")
	if out, err := run(ctx, "cat "+pidPath+" 2>/dev/null"); err == nil {
		if pid := firstInt(out); pid > 0 {
			_, _ = run(ctx, "kill "+strconv.Itoa(pid)+" 2>/dev/null")
			_, _ = run(ctx, "rm -f "+pidPath)
			return nil
		}
	}
	// No PID file: fall back to a name-anchored kill so an unrelated process
	// whose name merely contains "crond" is never touched.
	_, _ = run(ctx, "pkill -x crond 2>/dev/null")
	_, _ = run(ctx, "rm -f "+pidPath)
	return nil
}

// firstInt extracts the first integer from s, or 0 when there is none.
func firstInt(s string) int {
	for _, field := range strings.Fields(s) {
		if n, err := strconv.Atoi(field); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// isSafeToken reports whether s is a bare command name safe to interpolate
// into a shell probe. Anything with a metacharacter is rejected.
func isSafeToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '_', c == '.', c == '+', c == '/':
		default:
			return false
		}
	}
	return true
}

// quoteSingle wraps s in single quotes, escaping embedded quotes for POSIX sh.
func quoteSingle(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// truncateTail keeps at most n bytes from the end of s, which preserves the
// part of a failing command's output that usually matters.
func truncateTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := len(s) - n
	// Avoid splitting a UTF-8 rune: advance to the next continuation boundary.
	for cut < len(s) && s[cut]&0xC0 == 0x80 {
		cut++
	}
	return s[cut:]
}
