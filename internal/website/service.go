package website

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"termux-monitor/internal/store"
)

// Runtime timings. The grace period after a stop lets a shutting-down engine
// release its port before a restart tries to bind it again.
const (
	// ProbeTimeout bounds short liveness probes (command -v, kill -0, version).
	ProbeTimeout = 8 * time.Second
	// StopGrace is how long a restart waits after killing the old process.
	StopGrace = 500 * time.Millisecond
	// LogTailBytes caps how much of a site log is returned to the UI.
	LogTailBytes = 32768
	// startAttempts and startInterval bound how long Start waits for a
	// launched process to prove it is still alive before reporting failure.
	// The window covers the gap between fork and the engine's first syscall.
	startAttempts = 12
	startInterval = 250 * time.Millisecond
)

// Service owns the site registry, the per-site runtime state and the shell
// execution that starts and stops engines as the Termux user.
type Service struct {
	registry *Registry

	mu            sync.Mutex
	state         map[string]*siteState
	startAttempts int
	startInterval time.Duration
}

// siteState is the in-memory runtime bookkeeping for one site.
type siteState struct {
	startedAt time.Time
	version   string
	versionOK bool
}

// NewService wires the domain layer to a registry.
func NewService(registry *Registry) *Service {
	return &Service{
		registry:      registry,
		state:         make(map[string]*siteState),
		startAttempts: startAttempts,
		startInterval: startInterval,
	}
}

// Available reports whether the Termux environment is usable, plus a reason.
func (s *Service) Available() (bool, string) {
	reason := store.AvailableReason()
	return reason == "", reason
}

// termuxCommand builds a command running script as the Termux user.
func termuxCommand(ctx context.Context, script string) (*exec.Cmd, error) {
	return store.TermuxCommand(ctx, script)
}

// run executes script through the Termux shell and returns its combined output.
func (s *Service) run(ctx context.Context, script string) (string, error) {
	cmd, err := termuxCommand(ctx, script)
	if err != nil {
		return "", err
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// probe runs a short-lived command with the shared probe deadline.
func (s *Service) probe(script string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ProbeTimeout)
	defer cancel()
	return s.run(ctx, script)
}

// runUser executes script in the server's own identity with the Termux
// environment applied. It is used for the long-running engine processes, which
// must run outside the restricted Magisk SELinux domain that `su <uid>` gives
// their children (see Start). The extra environment variables are exported
// ahead of the script so engines that read their listen port from the
// environment (the common `process.env.PORT` convention) bind the right port.
func (s *Service) runUser(script string, env map[string]string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ProbeTimeout)
	defer cancel()
	for name, value := range env {
		script = "export " + name + "=" + shellQuote(value) + "; " + script
	}
	cmd, err := store.TermuxUserCommand(ctx, script)
	if err != nil {
		return "", err
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// pidFile returns the runtime path holding one site's PID.
func pidFile(id string) string {
	return store.TermuxRuntime("site-" + id + ".pid")
}

// logFile returns the runtime path holding one site's process output.
func logFile(id string) string {
	return store.TermuxRuntime("site-" + id + ".log")
}

// PID returns the live PID of a site, or 0 when it is not running. It trusts
// the PID file and verifies the process still exists before reporting it.
//
// Both the read and the stale-file cleanup happen in the server's own identity
// rather than through `su <uid>`: the engine is started by the server (see
// Start), so the PID file belongs to the server, and the UI polls this method
// every few seconds — spawning a `su` for each poll would be wasteful.
func (s *Service) PID(id string) int {
	if !validID(id) {
		return 0
	}
	data, err := os.ReadFile(pidFile(id))
	if err != nil {
		return 0
	}
	pid := firstInt(string(data))
	if pid <= 0 {
		return 0
	}
	if err := s.checkAlive(id, pid); err != nil {
		// Stale PID file: the process died without cleanup.
		_ = os.Remove(pidFile(id))
		return 0
	}
	return pid
}

// EngineInstalled reports whether a site's engine binary resolves inside the
// Termux $PREFIX.
func (s *Service) EngineInstalled(engine string) bool {
	bin := EngineBin(engine)
	_, err := s.probe("command -v " + bin + " >/dev/null 2>&1")
	return err == nil
}

// engineVersion probes an engine's version string once per service lifetime and
// caches it, since the UI polls the listing every few seconds.
func (s *Service) engineVersion(engine string) (string, bool) {
	s.mu.Lock()
	st, ok := s.state[engine]
	if !ok {
		st = &siteState{}
		s.state[engine] = st
	}
	if st.versionOK {
		version, installed := st.version, st.version != ""
		s.mu.Unlock()
		return version, installed
	}
	s.mu.Unlock()

	bin := EngineBin(engine)
	out, err := s.probe(bin + " --version 2>/dev/null | head -n 1")
	version := strings.TrimSpace(out)
	installed := err == nil && version != ""

	s.mu.Lock()
	if s.state[engine] == nil {
		s.state[engine] = &siteState{}
	}
	st = s.state[engine]
	st.version = version
	st.versionOK = true
	s.mu.Unlock()
	return version, installed
}

// List returns every site enriched with its runtime state.
func (s *Service) List(hostname string) []SiteView {
	sites := s.registry.List()
	out := make([]SiteView, 0, len(sites))
	for _, site := range sites {
		out = append(out, s.view(site, hostname))
	}
	return out
}

// view builds the enriched representation of one site.
func (s *Service) view(site Site, hostname string) SiteView {
	version, installed := s.engineVersion(site.Engine)
	pid := 0
	if installed {
		pid = s.PID(site.ID)
	}
	view := SiteView{
		Site:          site,
		Running:       pid > 0,
		PID:           pid,
		EngineOK:      installed,
		EngineVersion: version,
	}
	if view.Running {
		view.URL = siteURL(hostname, site.Port)
		s.mu.Lock()
		if st, ok := s.state[site.ID]; ok && !st.startedAt.IsZero() {
			view.UptimeMs = time.Since(st.startedAt).Milliseconds()
		}
		s.mu.Unlock()
	}
	return view
}

// siteURL builds the address a site is reachable at. A blank hostname falls
// back to localhost so the link is always well formed.
func siteURL(hostname string, port int) string {
	if strings.TrimSpace(hostname) == "" {
		return "http://127.0.0.1:" + strconv.Itoa(port)
	}
	return "http://" + hostname + ":" + strconv.Itoa(port)
}

// Find returns the raw site record for an id.
func (s *Service) Find(id string) (Site, bool) {
	if !validID(id) {
		return Site{}, false
	}
	return s.registry.Find(id)
}

// Add validates and stores a new site.
func (s *Service) Add(name, engine, path string, port int) (Site, error) {
	return s.registry.Add(name, engine, path, port)
}

// Delete stops a site (best effort) and removes it from the registry.
func (s *Service) Delete(id string) error {
	if !validID(id) {
		return ErrUnknownSite
	}
	if _, ok := s.registry.Find(id); !ok {
		return ErrUnknownSite
	}
	// A running site holds a port; stop it before forgetting the definition so
	// a later re-add on the same port can bind.
	_ = s.Stop(id)
	if err := s.registry.Delete(id); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.state, id)
	s.mu.Unlock()
	_ = os.Remove(logFile(id))
	return nil
}

// Start launches a site's engine, detached from the server.
//
// The engine is spawned by the server itself (root): `su <uid>` leaves the
// child in the Magisk SELinux domain, which Android denies `name_bind` on every
// TCP port, so an engine started as the Termux user can never reach its listen
// call. The child is reparented away from the Go process, so exec.Cmd cannot
// reap or kill it: it is started with `setsid nohup ... &` and its
// shell-assigned PID is recorded in a PID file for later Stop/Status.
//
// The runtime directory and log file are prepared before the engine runs, and
// after launching the recorded PID is verified for liveness so a start that
// silently failed reports an error instead of a false success.
func (s *Service) Start(id string) error {
	site, ok := s.registry.Find(id)
	if !ok {
		return ErrUnknownSite
	}
	if s.PID(id) > 0 {
		return nil // Already running: starting again is a no-op success.
	}
	if !s.EngineInstalled(site.Engine) {
		return ErrEngineAbsent
	}

	// The engine binds a TCP port. Android's SELinux policy denies `name_bind`
	// to the Magisk domain that `su <uid> -c` leaves us in, so an engine
	// launched as the Termux user fails with EACCES on every port. The engine
	// is therefore launched directly by the server (root), whose network
	// namespace is unrestricted; only reads of the site tree are needed, so no
	// Termux-side write access is required.
	if err := store.PrepareRuntimeDir(); err != nil {
		return err
	}

	// Bind the engine to loopback only. Exposing a phone server to the LAN is
	// the Tunnel/NAS tab's job, not an unauthenticated file server's.
	command := EngineCommand(site)
	cwd := WorkingDir(site)
	script := "cd " + shellQuote(cwd) + " >/dev/null 2>&1" +
		"; setsid nohup sh -c " + shellQuote(command) +
		" >> " + shellQuote(logFile(id)) + " 2>&1 < /dev/null & echo $!"
	// Truncate the previous log before the new process appends to it, so a
	// restart does not interleave two runs' output. The log lives in the
	// server's own runtime area (see PID), so it is truncated directly.
	_ = os.Truncate(logFile(id), 0)

	out, err := s.runUser(script, map[string]string{
		"PORT": strconv.Itoa(site.Port),
		"HOST": "127.0.0.1",
	})
	if err != nil {
		return err
	}
	pid := firstInt(out)
	if pid <= 0 {
		return ErrStartFailed
	}
	if err := store.WriteStateFile("site-"+id+".pid", strconv.Itoa(pid)); err != nil {
		return err
	}
	// Guard against a false positive: the shell reports a PID for a process
	// that may die immediately (a bad command, a missing interpreter, a port
	// already in use or a sandbox denial). The child needs a moment to reach
	// its listen call, so probe liveness a few times across a short window
	// before declaring success; a process that survives the window counts as
	// started even when its log later reports a runtime error.
	if !s.awaitAlive(id, pid) {
		_ = os.Remove(pidFile(id))
		return ErrStartFailed
	}
	s.mu.Lock()
	s.state[id] = &siteState{startedAt: time.Now()}
	s.mu.Unlock()
	return nil
}

// SetStartProbe overrides how long Start waits for a launched process to prove
// it is alive, and how often it checks. It exists for tests; callers in
// production use the package defaults.
func (s *Service) SetStartProbe(attempts int, interval time.Duration) {
	s.mu.Lock()
	s.startAttempts = attempts
	s.startInterval = interval
	s.mu.Unlock()
}

// awaitAlive polls for the given PID until it is observed alive or the attempt
// budget runs out. It reports false only when every probe failed, which means
// the process never lived.
func (s *Service) awaitAlive(id string, pid int) bool {
	s.mu.Lock()
	attempts := s.startAttempts
	interval := s.startInterval
	s.mu.Unlock()
	if attempts < 1 {
		attempts = 1
	}
	for i := 0; i < attempts; i++ {
		if err := s.checkAlive(id, pid); err == nil {
			return true
		}
		if i < attempts-1 {
			time.Sleep(interval)
		}
	}
	return false
}

// checkAlive reports whether the given PID is still alive. The actual probe is
// platform-specific (see processAlive) so the package keeps compiling on the
// developer's desktop while using the correct syscall on the phone.
func (s *Service) checkAlive(id string, pid int) error {
	return processAlive(pid)
}

// Stop kills the process recorded in a site's PID file. Unknown or already
// stopped sites are a no-op success.
func (s *Service) Stop(id string) error {
	if !validID(id) {
		return ErrUnknownSite
	}
	pid := s.PID(id)
	if pid > 0 {
		// Kill the process group: the engine may itself have spawned children.
		// Signals come from the server process itself, which is the parent of
		// the engine's session and therefore allowed to signal it.
		_ = s.signal(syscall.SIGTERM, pid)
		// Give it a moment to die cleanly, then force it.
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if s.PID(id) == 0 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if s.PID(id) > 0 {
			_ = s.signal(syscall.SIGKILL, pid)
		}
	}
	_ = os.Remove(pidFile(id))
	s.mu.Lock()
	delete(s.state, id)
	s.mu.Unlock()
	return nil
}

// signal sends a signal to the given PID's process group, falling back to the
// bare PID when the process is not a group leader. The actual delivery is
// platform-specific (see signalProcess) so the package keeps compiling on the
// developer's desktop while using the correct syscall on the phone.
func (s *Service) signal(sig syscall.Signal, pid int) error {
	return signalProcess(sig, pid)
}

// Restart stops a site and starts it again after a short grace period so the
// engine releases its listening port before it is rebound.
func (s *Service) Restart(id string) error {
	if _, ok := s.registry.Find(id); !ok {
		return ErrUnknownSite
	}
	if err := s.Stop(id); err != nil {
		return err
	}
	time.Sleep(StopGrace)
	return s.Start(id)
}

// Logs returns the tail of a site's captured output. A site that never started
// yields an empty string rather than an error.
func (s *Service) Logs(id string) (string, error) {
	if !validID(id) {
		return "", ErrUnknownSite
	}
	if _, ok := s.registry.Find(id); !ok {
		return "", ErrUnknownSite
	}
	// The log is written by the engine the server spawned, so it is read
	// directly rather than through `su <uid>` (see PID).
	data, err := os.ReadFile(logFile(id))
	if err != nil {
		return "", nil
	}
	if len(data) > LogTailBytes {
		data = data[len(data)-LogTailBytes:]
	}
	return string(data), nil
}

// RunningCount reports how many sites are currently running, used to decide
// whether the UI should keep polling.
func (s *Service) RunningCount() int {
	n := 0
	for _, site := range s.registry.List() {
		if s.PID(site.ID) > 0 {
			n++
		}
	}
	return n
}

// firstInt extracts the first positive integer from s, or 0 when there is none.
func firstInt(s string) int {
	for _, field := range strings.Fields(s) {
		if n, err := strconv.Atoi(field); err == nil && n > 0 {
			return n
		}
	}
	return 0
}
