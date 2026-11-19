package terminal_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"termux-monitor/internal/auth"
	"termux-monitor/internal/terminal"
)

// fakeBackend records what the manager sends and lets tests push output.
type fakeBackend struct {
	mu      sync.Mutex
	writes  []string
	resizes [][2]uint16
	killed  bool

	output    func(chunk []byte)
	exit      func()
	exitSeen  bool
	exitFired bool
}

func (b *fakeBackend) Write(data string) {
	b.mu.Lock()
	b.writes = append(b.writes, data)
	b.mu.Unlock()
}

func (b *fakeBackend) Resize(cols, rows uint16) {
	b.mu.Lock()
	b.resizes = append(b.resizes, [2]uint16{cols, rows})
	b.mu.Unlock()
}

func (b *fakeBackend) Kill() {
	b.mu.Lock()
	b.killed = true
	b.mu.Unlock()
}

func (b *fakeBackend) OnOutput(cb func(chunk []byte)) {
	b.mu.Lock()
	b.output = cb
	b.mu.Unlock()
}

// OnExit mirrors the real backends: a shell that already exited before the
// callback was wired still notifies the owner.
func (b *fakeBackend) OnExit(cb func()) {
	b.mu.Lock()
	b.exit = cb
	fired := b.exitFired
	b.mu.Unlock()
	if fired {
		cb()
	}
}

func (b *fakeBackend) emit(chunk string) {
	b.mu.Lock()
	cb := b.output
	b.mu.Unlock()
	if cb != nil {
		cb([]byte(chunk))
	}
}

func (b *fakeBackend) signalExit() {
	b.mu.Lock()
	if b.exitFired {
		b.mu.Unlock()
		return
	}
	b.exitFired = true
	b.exitSeen = true
	cb := b.exit
	b.mu.Unlock()
	if cb != nil {
		cb()
	}
}

func (b *fakeBackend) lastWrite() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.writes) == 0 {
		return ""
	}
	return b.writes[len(b.writes)-1]
}

func (b *fakeBackend) killedFlag() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.killed
}

// fakeFactory hands out fake backends and remembers creation order.
type fakeFactory struct {
	mu       sync.Mutex
	made     []*fakeBackend
	pipeOnly bool // force the pty path to fail and exercise the fallback
}

func (f *fakeFactory) CreatePty(_ terminal.ShellSpec) terminal.Backend {
	if f.pipeOnly {
		return nil
	}
	return f.add()
}

func (f *fakeFactory) CreatePipe(_ terminal.ShellSpec) terminal.Backend {
	return f.add()
}

func (f *fakeFactory) add() *fakeBackend {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := &fakeBackend{}
	f.made = append(f.made, b)
	return b
}

func (f *fakeFactory) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.made)
}

func (f *fakeFactory) at(i int) *fakeBackend {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i >= len(f.made) {
		return nil
	}
	return f.made[i]
}

// sessionCookie initialises a fresh auth manager and logs in with the default
// password, returning the auth cookie used by the websocket dialer.
func sessionCookie(t *testing.T) *http.Cookie {
	t.Helper()
	mgr := auth.NewManager(auth.DefaultPassword)
	token, ok, limited := mgr.Authenticate(auth.DefaultPassword, "127.0.0.1")
	if !ok || limited {
		t.Fatalf("failed to authenticate test session (ok=%v limited=%v)", ok, limited)
	}
	return &http.Cookie{Name: auth.CookieName, Value: token}
}

// socketEnv is a dialable test server bound to a fake-backed manager.
type socketEnv struct {
	conn   *websocket.Conn
	mgr    *terminal.Manager
	url    string
	cookie *http.Cookie
	t      *testing.T

	srv *httptest.Server
}

// newSocket spins up a WS server bound to a fake-backed manager.
func newSocket(t *testing.T, factory *fakeFactory) *socketEnv {
	t.Helper()
	t.Setenv("TMON_DATA_DIR", t.TempDir())

	mgr := terminal.NewManager(factory)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		terminal.HandleWebsocketWith(mgr, w, r)
	}))

	env := &socketEnv{
		mgr:    mgr,
		url:    "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/terminal",
		cookie: sessionCookie(t),
		t:      t,
		srv:    srv,
	}
	conn, err := env.dial()
	if err != nil {
		srv.Close()
		t.Fatalf("dial terminal websocket: %v", err)
	}
	env.conn = conn

	t.Cleanup(func() {
		conn.Close()
		mgr.Shutdown()
		srv.Close()
	})
	return env
}

func (e *socketEnv) dial() (*websocket.Conn, error) {
	hdr := http.Header{}
	hdr.Add("Cookie", e.cookie.String())
	conn, _, err := websocket.DefaultDialer.Dial(e.url, hdr)
	return conn, err
}

// readAll drains frames until the deadline, joining their payloads.
func readAll(t *testing.T, conn *websocket.Conn, d time.Duration) string {
	t.Helper()
	var sb strings.Builder
	deadline := time.Now().Add(d)
	for {
		_ = conn.SetReadDeadline(deadline)
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return sb.String()
		}
		sb.Write(msg)
	}
}

// waitFor polls cond until it is true or the timeout elapses.
func waitFor(t *testing.T, d time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

func TestManager_FirstAttachSpawnsPtySession(t *testing.T) {
	f := &fakeFactory{}
	env := newSocket(t, f)

	out := readAll(t, env.conn, 300*time.Millisecond)
	if f.count() != 1 {
		t.Fatalf("expected exactly one backend, got %d", f.count())
	}
	if !strings.Contains(out, "Terminal ready (") || !strings.Contains(out, ", pty)") {
		t.Errorf("expected pty ready banner, got %q", out)
	}
}

func TestManager_InputAndResizeProtocol(t *testing.T) {
	f := &fakeFactory{}
	env := newSocket(t, f)

	if !waitFor(t, time.Second, func() bool { return f.count() == 1 }) {
		t.Fatal("backend was not created")
	}
	be := f.at(0)

	_ = env.conn.WriteMessage(websocket.TextMessage, []byte(`{"t":"in","d":"id\n"}`))
	if !waitFor(t, time.Second, func() bool { return be.lastWrite() == "id\n" }) {
		t.Errorf("expected input write, got %q", be.lastWrite())
	}

	_ = env.conn.WriteMessage(websocket.TextMessage, []byte(`{"t":"rs","c":120,"r":40}`))
	if !waitFor(t, time.Second, func() bool {
		be.mu.Lock()
		defer be.mu.Unlock()
		return len(be.resizes) == 1
	}) {
		t.Fatal("resize was not applied")
	}
	be.mu.Lock()
	got := be.resizes[0]
	be.mu.Unlock()
	if got != [2]uint16{120, 40} {
		t.Errorf("expected resize 120x40, got %v", got)
	}

	// Zero dimensions fall back to the default 80x24 geometry instead of
	// collapsing the pty.
	_ = env.conn.WriteMessage(websocket.TextMessage, []byte(`{"t":"rs","c":0,"r":0}`))
	if !waitFor(t, time.Second, func() bool {
		be.mu.Lock()
		defer be.mu.Unlock()
		return len(be.resizes) == 2
	}) {
		t.Fatal("fallback resize was not applied")
	}
	be.mu.Lock()
	fallback := be.resizes[1]
	be.mu.Unlock()
	if fallback != [2]uint16{80, 24} {
		t.Errorf("expected fallback resize 80x24, got %v", fallback)
	}
}

func TestManager_RawKeystrokesFallback(t *testing.T) {
	f := &fakeFactory{}
	env := newSocket(t, f)

	if !waitFor(t, time.Second, func() bool { return f.count() == 1 }) {
		t.Fatal("backend was not created")
	}
	be := f.at(0)
	_ = env.conn.WriteMessage(websocket.TextMessage, []byte("ls\r"))
	if !waitFor(t, time.Second, func() bool { return be.lastWrite() == "ls\r" }) {
		t.Errorf("expected raw keystrokes to reach the shell, got %q", be.lastWrite())
	}
}

func TestManager_NewSessionReplacesBackend(t *testing.T) {
	f := &fakeFactory{}
	env := newSocket(t, f)

	if !waitFor(t, time.Second, func() bool { return f.count() == 1 }) {
		t.Fatal("first backend was not created")
	}
	first := f.at(0)

	_ = env.conn.WriteMessage(websocket.TextMessage, []byte(`{"t":"new"}`))
	if !waitFor(t, time.Second, func() bool { return f.count() == 2 }) {
		t.Fatalf("expected a replacement backend, got %d", f.count())
	}
	if !waitFor(t, time.Second, first.killedFlag) {
		t.Error("expected the previous shell to be killed")
	}
	out := readAll(t, env.conn, 300*time.Millisecond)
	if !strings.Contains(out, "New terminal session") {
		t.Errorf("expected new session banner, got %q", out)
	}
}

func TestManager_ReattachReplaysScrollback(t *testing.T) {
	f := &fakeFactory{}
	env := newSocket(t, f)

	if !waitFor(t, time.Second, func() bool { return f.count() == 1 }) {
		t.Fatal("backend was not created")
	}
	be := f.at(0)
	be.emit("hello-from-shell")
	_ = readAll(t, env.conn, 200*time.Millisecond)

	// Closing the browser tab must not kill the shell.
	env.conn.Close()
	time.Sleep(150 * time.Millisecond)
	if be.killedFlag() {
		t.Fatal("shell must survive client disconnect")
	}

	conn2, err := env.dial()
	if err != nil {
		t.Fatalf("reconnect failed: %v", err)
	}
	defer conn2.Close()

	out := readAll(t, conn2, 300*time.Millisecond)
	if !strings.Contains(out, "hello-from-shell") {
		t.Errorf("expected scrollback replay, got %q", out)
	}
	if !strings.Contains(out, "Reattached to terminal session") {
		t.Errorf("expected reattach banner, got %q", out)
	}
	if f.count() != 1 {
		t.Errorf("reconnect must reuse the shell, backends=%d", f.count())
	}
}

func TestManager_ExitBanner(t *testing.T) {
	f := &fakeFactory{}
	env := newSocket(t, f)

	if !waitFor(t, time.Second, func() bool { return f.count() == 1 }) {
		t.Fatal("backend was not created")
	}
	f.at(0).signalExit()
	out := readAll(t, env.conn, 300*time.Millisecond)
	if !strings.Contains(out, "session ended") {
		t.Errorf("expected session ended banner, got %q", out)
	}
}

func TestManager_PipeModeBanner(t *testing.T) {
	f := &fakeFactory{pipeOnly: true}
	env := newSocket(t, f)

	out := readAll(t, env.conn, 300*time.Millisecond)
	if !strings.Contains(out, "Terminal ready (") || !strings.Contains(out, ", pipe)") {
		t.Errorf("expected pipe ready banner, got %q", out)
	}
}

func TestManager_NewSessionMigratesAllClients(t *testing.T) {
	f := &fakeFactory{}
	env := newSocket(t, f)

	conn2, err := env.dial()
	if err != nil {
		t.Fatalf("second dial failed: %v", err)
	}
	defer conn2.Close()

	if !waitFor(t, time.Second, func() bool { return f.count() == 1 }) {
		t.Fatal("backend was not created")
	}

	// The first client asks for a new session.
	_ = env.conn.WriteMessage(websocket.TextMessage, []byte(`{"t":"new"}`))
	if !waitFor(t, time.Second, func() bool { return f.count() == 2 }) {
		t.Fatalf("expected a replacement backend, got %d", f.count())
	}

	// The second client must follow the shell to the new session.
	f.at(1).emit("migrated")
	out := readAll(t, conn2, 300*time.Millisecond)
	if !strings.Contains(out, "migrated") {
		t.Errorf("expected the second client to follow the new session, got %q", out)
	}
}

func TestManager_UnknownJSONFallsThroughToShell(t *testing.T) {
	f := &fakeFactory{}
	env := newSocket(t, f)

	if !waitFor(t, time.Second, func() bool { return f.count() == 1 }) {
		t.Fatal("backend was not created")
	}
	be := f.at(0)

	// A JSON object without a known "t" is written to the shell verbatim.
	_ = env.conn.WriteMessage(websocket.TextMessage, []byte(`{"foo":1}`))
	if !waitFor(t, time.Second, func() bool { return be.lastWrite() == `{"foo":1}` }) {
		t.Errorf("expected the raw frame to reach the shell, got %q", be.lastWrite())
	}
}

func TestManager_InputAfterExitStartsNewSession(t *testing.T) {
	f := &fakeFactory{}
	env := newSocket(t, f)

	if !waitFor(t, time.Second, func() bool { return f.count() == 1 }) {
		t.Fatal("backend was not created")
	}
	f.at(0).signalExit()

	out := readAll(t, env.conn, 300*time.Millisecond)
	if !strings.Contains(out, "session ended") {
		t.Fatalf("expected the exit banner, got %q", out)
	}

	// The next input starts a fresh shell instead of being dropped.
	_ = env.conn.WriteMessage(websocket.TextMessage, []byte(`{"t":"in","d":"echo hi\n"}`))
	if !waitFor(t, time.Second, func() bool { return f.count() == 2 }) {
		t.Fatalf("expected a replacement backend, got %d", f.count())
	}
	be := f.at(1)
	if !waitFor(t, time.Second, func() bool { return be.lastWrite() == "echo hi\n" }) {
		t.Errorf("expected the input to reach the new shell, got %q", be.lastWrite())
	}
}

func TestManager_ShutdownKillsShell(t *testing.T) {
	f := &fakeFactory{}
	env := newSocket(t, f)

	if !waitFor(t, time.Second, func() bool { return f.count() == 1 }) {
		t.Fatal("backend was not created")
	}
	env.mgr.Shutdown()
	if !f.at(0).killedFlag() {
		t.Error("shutdown must kill the live shell")
	}
}
