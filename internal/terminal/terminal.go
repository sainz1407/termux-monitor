package terminal

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"termux-monitor/internal/auth"
)

// ScrollbackMax caps the replayable output per session (64 KB, olp parity).
const ScrollbackMax = 65536

// Input message types accepted from the client. Anything that is not valid
// JSON is written to the shell verbatim (legacy clients).
const (
	msgInput   = "in"
	msgResize  = "rs"
	msgNewSess = "new"
)

// Session banners. The shapes mirror OpenLocalPanel's terminal banners so
// the xterm client can parse the shell and transport mode out of them.
const (
	bannerFresh = "\r\n=== Terminal ready (%s, %s) — persistent session, reload keeps the shell alive ===\r\n"
	bannerNew   = "\r\n=== New terminal session (%s, %s) ===\r\n"
	bannerReatt = "\r\n=== Reattached to terminal session (%s, %s) ===\r\n"
	bannerExit  = "\r\n=== Terminal session ended (shell exit). Reconnect or send input for a new session. ===\r\n"
	bannerFail  = "\r\n=== Failed to start shell ===\r\n"
)

// Default terminal geometry, used when a client omits a dimension.
const (
	defaultCols uint16 = 80
	defaultRows uint16 = 24
)

type clientMessage struct {
	T string          `json:"t"`
	D json.RawMessage `json:"d"`
	C uint16          `json:"c"`
	R uint16          `json:"r"`
}

// Manager owns exactly one live shell. Clients attach to it, receive the
// scrollback, and only detach on disconnect; the shell keeps running until
// a new session is requested or the server shuts down.
type Manager struct {
	mu      sync.Mutex
	factory BackendFactory
	session *session
}

type session struct {
	backend Backend
	mode    string // "pty" or "pipe"
	shell   string // human-readable shell label, e.g. "su" or "busybox sh"
	scroll  []byte
	clients map[*client]struct{}
	closed  bool
}

type client struct {
	send func([]byte) error
	sess *session
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return strings.EqualFold(u.Host, r.Host)
	},
}

var (
	globalMu      sync.Mutex
	globalManager *Manager
)

// DefaultManager returns the process-wide terminal manager, creating it on
// first use with the real pty/pipe factory.
func DefaultManager() *Manager {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalManager == nil {
		globalManager = NewManager(realFactory{})
	}
	return globalManager
}

// NewManager builds a manager with an injected backend factory.
// A nil factory selects the production pty/pipe factory.
func NewManager(factory BackendFactory) *Manager {
	if factory == nil {
		factory = realFactory{}
	}
	return &Manager{factory: factory}
}

// Shutdown kills the live shell and clears the session.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	s := m.session
	m.session = nil
	if s != nil {
		s.closed = true
	}
	m.mu.Unlock()
	if s != nil {
		s.backend.Kill()
	}
}

// spawnLocked starts a new shell, replacing any previous one, and returns
// it. The caller must hold m.mu.
func (m *Manager) spawnLocked() *session {
	if m.session != nil {
		old := m.session
		old.closed = true
		m.session = nil
		go old.backend.Kill()
	}

	backend, mode, shell := m.buildBackend()
	if backend == nil {
		return nil
	}
	s := &session{
		backend: backend,
		mode:    mode,
		shell:   shell,
		clients: make(map[*client]struct{}),
	}
	backend.OnOutput(func(chunk []byte) { m.broadcast(s, chunk) })
	// The exit callback may fire synchronously when the shell died before the
	// callback was wired, so it must never run while m.mu is held.
	backend.OnExit(func() { go m.handleExit(s) })
	m.session = s
	return s
}

// buildBackend tries each shell candidate with a pty, then falls back to
// pipes (no tty dimensions) for SELinux-blocked /dev/ptmx. It also returns
// the label of the candidate that started, for the session banner.
func (m *Manager) buildBackend() (Backend, string, string) {
	candidates := shellCandidates()
	for _, cand := range candidates {
		if b := m.factory.CreatePty(cand); b != nil {
			return b, "pty", cand.Label
		}
	}
	for _, cand := range candidates {
		if b := m.factory.CreatePipe(cand); b != nil {
			return b, "pipe", cand.Label
		}
	}
	return nil, "", ""
}

// broadcast fans a chunk out to every attached client and records it in the
// scrollback ring.
func (m *Manager) broadcast(s *session, chunk []byte) {
	m.mu.Lock()
	if s.closed {
		m.mu.Unlock()
		return
	}
	s.scroll = append(s.scroll, chunk...)
	if len(s.scroll) > ScrollbackMax {
		s.scroll = s.scroll[len(s.scroll)-ScrollbackMax:]
	}
	targets := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		targets = append(targets, c)
	}
	m.mu.Unlock()

	for _, c := range targets {
		_ = c.send(chunk)
	}
}

// handleExit marks the session as ended and tells attached clients. The
// session stays registered so its clients can migrate to the next shell.
func (m *Manager) handleExit(s *session) {
	m.mu.Lock()
	if s.closed || m.session != s {
		m.mu.Unlock()
		return
	}
	s.closed = true
	targets := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		targets = append(targets, c)
	}
	m.mu.Unlock()

	banner := []byte(bannerExit)
	for _, c := range targets {
		_ = c.send(banner)
	}
}

// broadcastRaw delivers a chunk without touching the scrollback.
func (m *Manager) broadcastRaw(s *session, chunk string) {
	m.mu.Lock()
	targets := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		targets = append(targets, c)
	}
	m.mu.Unlock()
	for _, c := range targets {
		_ = c.send([]byte(chunk))
	}
}

// spawnFreshLocked replaces the live shell with a new one and migrates every
// client of the previous session to it, so a session reset never drops the
// other tabs. The caller must hold m.mu.
func (m *Manager) spawnFreshLocked() *session {
	old := m.session
	fresh := m.spawnLocked()
	if fresh == nil {
		return nil
	}
	if old != nil {
		for c := range old.clients {
			fresh.clients[c] = struct{}{}
			c.sess = fresh
		}
	}
	return fresh
}

// attach registers a client and replays the scrollback.
func (m *Manager) attach(c *client, newSession bool) {
	if newSession {
		m.respawn()
		return
	}

	m.mu.Lock()
	s := m.session
	fresh := false
	if s == nil || s.closed {
		s = m.spawnFreshLocked()
		if s == nil {
			m.mu.Unlock()
			_ = c.send([]byte(bannerFail))
			return
		}
		fresh = true
	}
	s.clients[c] = struct{}{}
	c.sess = s
	m.mu.Unlock()

	if fresh {
		m.broadcastRaw(s, fmt.Sprintf(bannerFresh, s.shell, s.mode))
		return
	}

	m.mu.Lock()
	replay := s.scroll
	dead := s.closed
	m.mu.Unlock()
	if dead {
		return
	}
	if len(replay) > 0 {
		_ = c.send(replay)
	}
	_ = c.send([]byte(fmt.Sprintf(bannerReatt, s.shell, s.mode)))
}

// respawn replaces the live shell with a new one and migrates every
// attached client to it, so a session reset never drops the other tabs.
func (m *Manager) respawn() {
	m.mu.Lock()
	fresh := m.spawnFreshLocked()
	if fresh == nil {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	m.broadcastRaw(fresh, fmt.Sprintf(bannerNew, fresh.shell, fresh.mode))
}

// detach removes a client without touching the shell.
func (m *Manager) detach(c *client) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.sess != nil {
		delete(c.sess.clients, c)
		c.sess = nil
	}
}

// handleMessage parses one client frame and applies it to the session.
func (m *Manager) handleMessage(c *client, data []byte) {
	var msg clientMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		// Legacy raw keystrokes.
		m.write(c, data)
		return
	}
	switch msg.T {
	case msgInput:
		var d string
		if err := json.Unmarshal(msg.D, &d); err != nil {
			d = ""
		}
		m.write(c, []byte(d))
	case msgResize:
		m.resize(c, resizeDim(msg.C, defaultCols), resizeDim(msg.R, defaultRows))
	case msgNewSess:
		m.respawn()
	default:
		m.write(c, data)
	}
}

// resizeDim falls back to the default terminal size when the client sends a
// zero or missing dimension.
func resizeDim(v uint16, fallback uint16) uint16 {
	if v == 0 {
		return fallback
	}
	return v
}

// write forwards client input to the live shell. When the previous shell has
// exited, the input starts a fresh session instead of being dropped.
func (m *Manager) write(c *client, data []byte) {
	if len(data) == 0 {
		return
	}
	m.mu.Lock()
	s := m.session
	fresh := false
	if s == nil || s.closed {
		s = m.spawnFreshLocked()
		if s == nil {
			m.mu.Unlock()
			_ = c.send([]byte(bannerFail))
			return
		}
		fresh = true
	}
	if _, ok := s.clients[c]; !ok {
		s.clients[c] = struct{}{}
		c.sess = s
	}
	m.mu.Unlock()

	if fresh {
		m.broadcastRaw(s, fmt.Sprintf(bannerFresh, s.shell, s.mode))
	}
	s.backend.Write(string(data))
}

func (m *Manager) resize(c *client, cols, rows uint16) {
	if cols == 0 || rows == 0 {
		return
	}
	m.mu.Lock()
	s := m.session
	m.mu.Unlock()
	if s == nil || s.closed {
		return
	}
	s.backend.Resize(cols, rows)
}

func HandleWebsocket(w http.ResponseWriter, r *http.Request) {
	HandleWebsocketWith(DefaultManager(), w, r)
}

// HandleWebsocketWith serves the terminal socket through an explicit manager.
// It exists so tests can inject a fake backend factory.
func HandleWebsocketWith(manager *Manager, w http.ResponseWriter, r *http.Request) {
	// Verify session authentication token before upgrading WS.
	// Cookie only: accepting the token via URL query would leak it into
	// server/proxy logs, browser history and referrers. Same-origin WS
	// clients (browsers) send the HttpOnly cookie automatically.
	token := ""
	if cookie, err := r.Cookie(auth.CookieName); err == nil && cookie.Value != "" {
		token = cookie.Value
	}
	if token == "" || !auth.GetManager().ValidateSession(token) {
		http.Error(w, "Unauthorized session", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Terminal WS upgrade error: %v", err)
		return
	}

	const (
		writeWait  = 10 * time.Second
		pongWait   = 60 * time.Second
		pingPeriod = (pongWait * 9) / 10
	)

	conn.SetReadLimit(512 * 1024)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	var writeMux sync.Mutex
	writeWS := func(data []byte) error {
		writeMux.Lock()
		defer writeMux.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
		return conn.WriteMessage(websocket.TextMessage, data)
	}

	stopPing := make(chan struct{})
	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				writeMux.Lock()
				_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
				err := conn.WriteMessage(websocket.PingMessage, nil)
				writeMux.Unlock()
				if err != nil {
					conn.Close()
					return
				}
			case <-stopPing:
				return
			}
		}
	}()

	c := &client{send: writeWS}
	defer func() {
		close(stopPing)
		manager.detach(c)
		conn.Close()
	}()

	manager.attach(c, false)

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if len(msg) == 0 {
			continue
		}
		manager.handleMessage(c, msg)
	}
}
