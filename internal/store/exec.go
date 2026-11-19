package store

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// Run holds the live state of one install or manager operation.
type Run struct {
	mu       sync.Mutex
	logs     []string
	running  bool
	exitCode int
	finished bool
}

// Push appends a log line, trimming the ring buffer to LogRing entries.
func (r *Run) Push(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, line)
	if len(r.logs) > LogRing {
		r.logs = r.logs[len(r.logs)-LogRing:]
	}
}

// Snapshot returns the current log text and run state.
func (r *Run) Snapshot() (logs string, running bool, exitCode int, finished bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	joined := strings.Join(r.logs, "")
	if r.finished {
		return joined, r.running, r.exitCode, true
	}
	return joined, r.running, 0, false
}

// IsRunning reports whether the run is still executing.
func (r *Run) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// SetExit records the terminal state of the run.
func (r *Run) SetExit(code int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running = false
	r.finished = true
	if err != nil {
		r.exitCode = -1
		r.logs = append(r.logs, "\n--- failed: "+err.Error()+" ---\n")
		return
	}
	r.exitCode = code
	r.logs = append(r.logs, "\n--- finished (exit "+itoa(code)+") ---\n")
}

// itoa avoids importing strconv in this file's hot path.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// Executor owns the run registry and the shell entry point.
type Executor struct {
	mu   sync.Mutex
	runs map[string]*Run // per-package installs, keyed by item id
	ops  map[string]*Run // manager operations: update, upgrade, purge
}

// NewExecutor creates an empty run registry.
func NewExecutor() *Executor {
	return &Executor{
		runs: make(map[string]*Run),
		ops:  make(map[string]*Run),
	}
}

// getOrCreate returns the run slot for a key, creating it when absent.
func (e *Executor) getOrCreate(m map[string]*Run, key string) *Run {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, ok := m[key]
	if !ok {
		r = &Run{}
		m[key] = r
	}
	return r
}

// RunFor returns the install run for an item id, or nil when it never ran.
func (e *Executor) RunFor(id string) *Run {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.runs[id]
}

// OpFor returns a manager operation run, or nil when it never ran.
func (e *Executor) OpFor(key string) *Run {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.ops[key]
}

// probe runs a short-lived command as the Termux user and returns its stdout.
// Absent environment or a non-zero exit is reported through the error.
func probe(ctx context.Context, script string) (string, error) {
	cmd, err := termuxCommand(ctx, suBin, script)
	if err != nil {
		return "", err
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// IsInstalled reports whether the item's binary is present in $PREFIX.
func IsInstalled(item Item) bool {
	ctx, cancel := context.WithTimeout(context.Background(), ProbeTimeout)
	defer cancel()
	if _, err := probe(ctx, item.checkCommand()); err != nil {
		return false
	}
	return true
}

// Version returns the installed version line for an item, or "" when the item
// is absent or the probe yields nothing. Errors map to an honest empty string.
func Version(item Item) string {
	if !IsInstalled(item) {
		return ""
	}
	vcmd := item.versionCommand()
	if vcmd == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), ProbeTimeout)
	defer cancel()
	out, err := probe(ctx, vcmd)
	if err != nil {
		return ""
	}
	line := out
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if len(line) > versionLineMax {
		line = line[:versionLineMax]
	}
	return line
}

// Launch starts script in the run slot identified by (map, key). It returns
// false when a run is already in flight for that key.
func (e *Executor) Launch(m map[string]*Run, key, script string) bool {
	run := e.getOrCreate(m, key)
	run.mu.Lock()
	if run.running {
		run.mu.Unlock()
		return false
	}
	run.logs = nil
	run.running = true
	run.finished = false
	run.exitCode = 0
	run.mu.Unlock()

	run.Push("$ " + script + "\n")

	cmd, err := termuxCommand(context.Background(), suBin, script)
	if err != nil {
		run.SetExit(0, err)
		return true
	}
	cmd.Env = buildEnv()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		run.SetExit(0, err)
		return true
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		run.SetExit(0, err)
		return true
	}
	if err := cmd.Start(); err != nil {
		run.SetExit(0, err)
		return true
	}

	var wg sync.WaitGroup
	stream := func(rc io.ReadCloser) {
		defer wg.Done()
		sc := bufio.NewScanner(rc)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			run.Push(sc.Text() + "\n")
		}
	}
	wg.Add(2)
	go stream(stdout)
	go stream(stderr)

	go func() {
		wg.Wait()
		err := cmd.Wait()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				run.SetExit(ee.ExitCode(), nil)
				return
			}
			run.SetExit(0, err)
			return
		}
		run.SetExit(0, nil)
	}()
	return true
}

// LaunchInstall starts an item installation.
func (e *Executor) LaunchInstall(item Item) bool {
	return e.Launch(e.runs, item.ID, item.installCommand())
}

// LaunchUpdate refreshes the package repository index.
func (e *Executor) LaunchUpdate() bool {
	return e.Launch(e.ops, "update", "pkg update")
}

// LaunchUpgrade upgrades all installed packages.
func (e *Executor) LaunchUpgrade() bool {
	return e.Launch(e.ops, "upgrade", "pkg upgrade -y")
}

// LaunchPurge clears the package cache.
func (e *Executor) LaunchPurge() bool {
	return e.Launch(e.ops, "purge", "apt-get clean")
}

// ReadLog serializes a run's current state for the API. A never-started run
// answers the empty shape rather than a 404, matching OLP behaviour.
func ReadLog(run *Run) map[string]any {
	if run == nil {
		return map[string]any{"logs": "", "running": false, "exitCode": nil}
	}
	logs, running, exitCode, finished := run.Snapshot()
	var code any
	if finished {
		code = exitCode
	}
	return map[string]any{"logs": logs, "running": running, "exitCode": code}
}
