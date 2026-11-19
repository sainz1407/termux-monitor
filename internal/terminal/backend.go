package terminal

import (
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

// Backend is the shell transport used by the manager. Implementations must
// never panic from lifecycle calls: a dead shell degrades into a clean exit.
// Output/exit callbacks are registered once, right after creation, and are
// invoked from backend goroutines.
type Backend interface {
	Write(data string)
	Resize(cols, rows uint16)
	Kill()
	OnOutput(cb func(chunk []byte))
	OnExit(cb func())
}

// BackendFactory creates shell transports. It stays a seam so manager tests
// never spawn a real process.
type BackendFactory interface {
	// CreatePty starts a pseudo-terminal for the spec; nil means the pty
	// could not be allocated or the shell failed to start.
	CreatePty(spec ShellSpec) Backend
	// CreatePipe starts the SELinux-resistant fallback (no tty dimensions).
	CreatePipe(spec ShellSpec) Backend
}

// realFactory is the production factory: pty first, pipe fallback.
type realFactory struct{}

func (realFactory) CreatePty(spec ShellSpec) Backend {
	cmd := shellRecipe(spec)
	master, err := pty.Start(cmd)
	if err != nil {
		return nil
	}
	b := &ptyBackend{cmd: cmd, master: master}
	b.launch()
	return b
}

func (realFactory) CreatePipe(spec ShellSpec) Backend {
	cmd := shellRecipe(spec)
	b := &pipeBackend{cmd: cmd}
	if err := b.launch(); err != nil {
		return nil
	}
	return b
}

// sinks holds the output/exit callbacks shared by both backends.
type sinks struct {
	mu     sync.Mutex
	output func(chunk []byte)
	exit   func()
	exited bool
}

// onOutput registers the output callback.
func (s *sinks) onOutput(cb func(chunk []byte)) {
	s.mu.Lock()
	s.output = cb
	s.mu.Unlock()
}

// onExit registers the exit callback. A shell that already exited before the
// callback was wired still notifies the owner, so a session never degrades
// into a dead shell that looks alive.
func (s *sinks) onExit(cb func()) {
	s.mu.Lock()
	s.exit = cb
	exited := s.exited
	s.mu.Unlock()
	if exited {
		cb()
	}
}

// emit delivers shell output to the registered callback.
func (s *sinks) emit(chunk []byte) {
	s.mu.Lock()
	cb := s.output
	s.mu.Unlock()
	if cb != nil {
		cb(chunk)
	}
}

// fireExit notifies the exit callback exactly once.
func (s *sinks) fireExit() {
	s.mu.Lock()
	if s.exited {
		s.mu.Unlock()
		return
	}
	s.exited = true
	cb := s.exit
	s.mu.Unlock()
	if cb != nil {
		cb()
	}
}

// ptyBackend streams a pseudo-terminal, giving job control, prompts and
// window size support.
type ptyBackend struct {
	cmd    *exec.Cmd
	master *os.File
	sinks
}

func (b *ptyBackend) launch() {
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := b.master.Read(buf)
			if n > 0 {
				b.emit(buf[:n])
			}
			if err != nil {
				break
			}
		}
		b.fireExit()
	}()
	go func() {
		_ = b.cmd.Wait()
		b.fireExit()
	}()
}

func (b *ptyBackend) Write(data string) {
	if data == "" {
		return
	}
	_, _ = b.master.Write([]byte(data))
}

func (b *ptyBackend) Resize(cols, rows uint16) {
	if cols == 0 || rows == 0 {
		return
	}
	_ = pty.Setsize(b.master, &pty.Winsize{Rows: rows, Cols: cols})
}

func (b *ptyBackend) Kill() {
	_ = b.master.Close()
	if b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
	}
}

func (b *ptyBackend) OnOutput(cb func(chunk []byte)) { b.onOutput(cb) }
func (b *ptyBackend) OnExit(cb func())               { b.onExit(cb) }

// pipeBackend is the plain stdin/stdout/stderr transport used when
// /dev/ptmx is blocked (SELinux). It has no tty dimensions.
type pipeBackend struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	out   io.Reader
	sinks
}

// launch wires the pipes and starts the command exactly once.
func (b *pipeBackend) launch() error {
	stdin, err := b.cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := b.cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := b.cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := b.cmd.Start(); err != nil {
		return err
	}
	b.stdin = stdin
	b.out = io.MultiReader(stdout, stderr)
	go func() {
		buf := make([]byte, 2048)
		for {
			n, err := b.out.Read(buf)
			if n > 0 {
				b.emit(buf[:n])
			}
			if err != nil {
				break
			}
		}
	}()
	go func() {
		_ = b.cmd.Wait()
		b.fireExit()
	}()
	return nil
}

func (b *pipeBackend) Write(data string) {
	if b.stdin == nil || data == "" {
		return
	}
	_, _ = b.stdin.Write([]byte(data))
}

func (b *pipeBackend) Resize(_, _ uint16) {}

func (b *pipeBackend) Kill() {
	if b.stdin != nil {
		_ = b.stdin.Close()
	}
	if b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
	}
}

func (b *pipeBackend) OnOutput(cb func(chunk []byte)) { b.onOutput(cb) }
func (b *pipeBackend) OnExit(cb func())               { b.onExit(cb) }
