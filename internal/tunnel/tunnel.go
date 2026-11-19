package tunnel

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"termux-monitor/internal/config"
	"termux-monitor/internal/logger"
)

type TunnelConfig struct {
	Engine            string `json:"engine"` // "cloudflare", "tailscale", "zerotier", "ngrok", "pinggy"
	Enabled           bool   `json:"enabled"`
	CloudflareToken   string `json:"cloudflare_token"`
	CloudflareQuick   bool   `json:"cloudflare_quick"`
	TailscaleAuthKey  string `json:"tailscale_auth_key"`
	ZeroTierNetworkID string `json:"zerotier_network_id"`
	NgrokAuthToken    string `json:"ngrok_auth_token"`
	PinggyToken       string `json:"pinggy_token"`
}

type TunnelStatus struct {
	Engine      string   `json:"engine"`
	Active      bool     `json:"active"`
	PublicURL   string   `json:"public_url"`
	IPAddress   string   `json:"ip_address"`
	Logs        []string `json:"logs"`
	BinaryFound bool     `json:"binary_found"`
	BinaryPath  string   `json:"binary_path"`
}

type Manager struct {
	mu           sync.RWMutex
	dataPath     string
	config       TunnelConfig
	active       bool
	publicURL    string
	ipAddress    string
	logs         []string
	cmd          *exec.Cmd
	cmdCancel    context.CancelFunc
	activeEngine string
	generation   uint64
}

var (
	globalManager *Manager
	once          sync.Once

	reURL       = regexp.MustCompile(`https://[a-zA-Z0-9-]+\.trycloudflare\.com`)
	reNgrokURL  = regexp.MustCompile(`https://[a-zA-Z0-9-]+\.ngrok-free\.app`)
	rePinggyURL = regexp.MustCompile(`https://[a-zA-Z0-9-]+\.pinggy\.link|https://[a-zA-Z0-9-]+\.a\.pinggy\.online`)

	localPortMu sync.RWMutex
	localPort   = 80

	// restartBaseDelay is the first backoff used when the engine process dies.
	restartBaseDelay = 5 * time.Second
)

// SetLocalPort tells the tunnel engines which local port the WebUI listens on.
func SetLocalPort(port int) {
	if port <= 0 || port > 65535 {
		return
	}
	localPortMu.Lock()
	localPort = port
	localPortMu.Unlock()
}

func currentLocalPort() int {
	localPortMu.RLock()
	defer localPortMu.RUnlock()
	return localPort
}

// localOrigin is the address the tunnel engines forward the public traffic to.
func localOrigin() string {
	return "http://localhost:" + strconv.Itoa(currentLocalPort())
}

func getStoragePath() string {
	return config.GetPersistentFilePath("tunnel.json")
}

func newManager() *Manager {
	m := &Manager{
		dataPath: getStoragePath(),
		config: TunnelConfig{
			Engine:          "cloudflare",
			Enabled:         false,
			CloudflareQuick: true,
		},
		logs: make([]string, 0),
	}
	_, _ = m.LoadConfig()
	return m
}

func GetManager() *Manager {
	once.Do(func() {
		globalManager = newManager()
	})
	return globalManager
}

// appendLog records a log line. The caller must hold m.mu.
func (m *Manager) appendLog(msg string) {
	entry := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg)
	m.logs = append(m.logs, entry)
	if len(m.logs) > 50 {
		m.logs = m.logs[len(m.logs)-50:]
	}
}

func (m *Manager) log(msg string) {
	m.mu.Lock()
	m.appendLog(msg)
	m.mu.Unlock()
}

func (m *Manager) LoadConfig() (*TunnelConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	buf, err := os.ReadFile(m.dataPath)
	if err != nil && m.dataPath != "tunnel.json" {
		buf, err = os.ReadFile("tunnel.json")
	}

	if err == nil {
		buf = bytes.TrimPrefix(buf, []byte("\xef\xbb\xbf"))
		var cfg TunnelConfig
		if err := json.Unmarshal(buf, &cfg); err == nil {
			if cfg.Engine == "" {
				cfg.Engine = "cloudflare"
			}
			m.config = cfg
			out := m.config
			return &out, nil
		}
	}

	out := m.config
	return &out, nil
}

func (m *Manager) SaveConfig(cfg *TunnelConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cfg == nil {
		return fmt.Errorf("config cannot be nil")
	}

	m.config = *cfg

	buf, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(m.dataPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		m.dataPath = "tunnel.json"
	}

	if err := config.WriteFileAtomic(m.dataPath, buf, 0644); err != nil {
		if m.dataPath != "tunnel.json" {
			_ = config.WriteFileAtomic("tunnel.json", buf, 0644)
		}
		return err
	}

	return nil
}

func GetBinDir() string {
	dir := filepath.Join(config.GetPersistentDataDir(), "bin")
	_ = os.MkdirAll(dir, 0755)
	return dir
}

// isExecutable reports whether the path can be executed by the daemon.
func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return false
	}
	// Windows has no executable bit, the file extension decides instead.
	if runtime.GOOS == "windows" {
		return true
	}
	return fi.Mode()&0111 != 0
}

// FindBinary searches for the tunnel binary in the module data dir, the daemon
// PATH, the Termux prefix and the standard Android / root manager directories.
func FindBinary(engine string) string {
	var names []string
	switch strings.ToLower(engine) {
	case "cloudflare":
		names = []string{"cloudflared"}
	case "tailscale":
		names = []string{"tailscale", "tailscaled"}
	case "zerotier":
		names = []string{"zerotier-one", "zerotier-cli"}
	case "ngrok":
		names = []string{"ngrok"}
	case "pinggy":
		names = []string{"ssh"}
	default:
		return ""
	}

	// 0. Search in persistent bin dir first
	binDir := GetBinDir()
	for _, name := range names {
		p := filepath.Join(binDir, name)
		if isExecutable(p) {
			return p
		}
	}

	// 1. Search in PATH
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil && isExecutable(path) {
			return path
		}
	}

	// 2. Search in the Termux prefix. The daemon runs as root with a minimal
	// PATH, so binaries installed through Termux are invisible to exec.LookPath
	// even though root can execute them.
	termuxDirs := []string{
		"/data/data/com.termux/files/usr/bin",
		"/data/data/com.termux/files/usr/sbin",
		"/data/data/com.termux/files/usr/local/bin",
		"/data/data/com.termux/files/home/bin",
	}
	if prefix := os.Getenv("PREFIX"); prefix != "" {
		termuxDirs = append([]string{filepath.Join(prefix, "bin"), filepath.Join(prefix, "sbin")}, termuxDirs...)
	}

	// 3. Search in standard Android / Magisk module binary locations
	searchDirs := append(termuxDirs,
		"/data/adb/modules/termux_monitor/bin",
		"/data/adb/modules/cloudflared/bin",
		"/data/adb/modules/tailscale/bin",
		"/data/adb/modules/zerotier/bin",
		"/data/adb/ap/bin",
		"/data/adb/ksu/bin",
		"/data/local/tmp",
		"/system/bin",
		"/system/xbin",
	)

	for _, dir := range searchDirs {
		for _, name := range names {
			p := filepath.Join(dir, name)
			if isExecutable(p) {
				return p
			}
		}
	}

	return ""
}

// DownloadBinary streams and saves a tunnel binary file into GetBinDir().
func DownloadBinary(engine string, reader io.Reader, fileName string) (string, error) {
	if reader == nil {
		return "", fmt.Errorf("reader cannot be nil")
	}

	engineLower := strings.ToLower(engine)
	targetName := ""
	switch engineLower {
	case "cloudflare":
		targetName = "cloudflared"
	case "ngrok":
		targetName = "ngrok"
	case "tailscale":
		targetName = "tailscale"
	case "zerotier":
		targetName = "zerotier-one"
	default:
		targetName = filepath.Base(fileName)
	}

	targetPath := filepath.Join(GetBinDir(), targetName)
	outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return "", fmt.Errorf("failed creating binary target path: %w", err)
	}
	defer outFile.Close()

	if _, err := io.Copy(outFile, reader); err != nil {
		_ = os.Remove(targetPath)
		return "", fmt.Errorf("failed saving binary file: %w", err)
	}

	// Grant executable permissions
	_ = os.Chmod(targetPath, 0755)
	logger.Get().Infof("Tunnel", "Binary %s installed successfully to %s", targetName, targetPath)
	return targetPath, nil
}

// buildArgs returns the command line arguments for a supervised engine.
func buildArgs(cfg TunnelConfig) ([]string, error) {
	switch strings.ToLower(cfg.Engine) {
	case "cloudflare":
		if cfg.CloudflareToken != "" && !cfg.CloudflareQuick {
			return []string{"tunnel", "run", "--token", cfg.CloudflareToken}, nil
		}
		// --no-autoupdate keeps the daemon from trying to replace a binary that
		// lives in the Termux prefix and is owned by another user.
		return []string{"tunnel", "--url", localOrigin(), "--no-autoupdate"}, nil
	case "ngrok":
		return []string{"http", strconv.Itoa(currentLocalPort()), "--log=stdout"}, nil
	case "pinggy":
		userHost := "a.pinggy.io"
		if cfg.PinggyToken != "" {
			userHost = cfg.PinggyToken + "@a.pinggy.io"
		}
		return []string{
			"-p", "443",
			"-R", "0:localhost:" + strconv.Itoa(currentLocalPort()),
			"-o", "StrictHostKeyChecking=no",
			"-o", "ServerAliveInterval=30",
			userHost,
		}, nil
	default:
		return nil, fmt.Errorf("engine %q cannot run as a supervised process", cfg.Engine)
	}
}

// tunnelEnv prepares the child environment. Binaries taken from the Termux
// prefix need the Termux HOME and PATH, otherwise they cannot resolve their
// own configuration directory.
func tunnelEnv(binPath string) []string {
	env := os.Environ()
	if !strings.HasPrefix(binPath, "/data/data/com.termux/") {
		return env
	}

	overrides := map[string]string{
		"HOME":   "/data/data/com.termux/files/home",
		"PREFIX": "/data/data/com.termux/files/usr",
		"PATH": "/data/data/com.termux/files/usr/bin:" +
			"/data/data/com.termux/files/usr/sbin:" + os.Getenv("PATH"),
	}

	out := make([]string, 0, len(env)+len(overrides))
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		if _, ok := overrides[key]; ok {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range overrides {
		out = append(out, k+"="+v)
	}
	return out
}

// interestingLine filters the very verbose engine output down to the lines
// that actually explain the state of the tunnel.
func interestingLine(line string) bool {
	upper := strings.ToUpper(line)
	return strings.Contains(upper, "ERR") ||
		strings.Contains(upper, "WRN") ||
		strings.Contains(upper, "REGISTERED TUNNEL CONNECTION") ||
		strings.Contains(upper, "TUNNEL HAS BEEN CREATED") ||
		strings.Contains(upper, "QUICK TUNNEL")
}

// scanOutput drains one engine output stream, publishing the public URL and
// keeping the relevant log lines.
func (m *Manager) scanOutput(r io.Reader, urlPattern *regexp.Regexp, engine string) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if match := urlPattern.FindString(line); match != "" {
			m.mu.Lock()
			m.publicURL = match
			m.appendLog(fmt.Sprintf("%s Public URL: %s", engine, match))
			m.mu.Unlock()
			continue
		}
		if interestingLine(line) {
			m.log(fmt.Sprintf("%s: %s", engine, line))
		}
	}
	if err := scanner.Err(); err != nil {
		logger.Get().Warnf("Tunnel", "%s output scan failed: %v", engine, err)
	}
}

// runOnce starts the engine process, streams its output and waits until it
// exits. The returned error describes why the process ended.
func (m *Manager) runOnce(ctx context.Context, gen uint64, cfg TunnelConfig, binPath string) error {
	args, err := buildArgs(cfg)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Env = tunnelEnv(binPath)

	outR, outW, err := os.Pipe()
	if err != nil {
		return err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		outR.Close()
		outW.Close()
		return err
	}
	cmd.Stdout = outW
	cmd.Stderr = errW

	if err := cmd.Start(); err != nil {
		outR.Close()
		outW.Close()
		errR.Close()
		errW.Close()
		return fmt.Errorf("failed starting %s: %w", cfg.Engine, err)
	}

	// The parent must release its copy of the write ends, otherwise the readers
	// never observe the EOF that follows the child exit.
	outW.Close()
	errW.Close()

	m.mu.Lock()
	if m.generation != gen {
		m.mu.Unlock()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil
	}
	m.cmd = cmd
	m.active = true
	m.publicURL = ""
	m.appendLog(fmt.Sprintf("Started %s tunnel via %s", cfg.Engine, binPath))
	m.mu.Unlock()

	pattern := reURL
	switch strings.ToLower(cfg.Engine) {
	case "ngrok":
		pattern = reNgrokURL
	case "pinggy":
		pattern = rePinggyURL
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		m.scanOutput(outR, pattern, cfg.Engine)
	}()
	go func() {
		defer wg.Done()
		m.scanOutput(errR, pattern, cfg.Engine)
	}()

	waitErr := cmd.Wait()
	wg.Wait()

	return waitErr
}

// supervise keeps the engine process alive. Whenever the child exits on its
// own the tunnel is restarted with an exponential backoff.
func (m *Manager) supervise(ctx context.Context, gen uint64, cfg TunnelConfig, binPath string) {
	defer func() {
		m.mu.Lock()
		if m.generation == gen {
			m.active = false
			m.cmd = nil
		}
		m.mu.Unlock()
	}()

	delay := restartBaseDelay
	for {
		err := m.runOnce(ctx, gen, cfg, binPath)

		if ctx.Err() != nil {
			return
		}
		m.mu.RLock()
		stale := m.generation != gen
		m.mu.RUnlock()
		if stale {
			return
		}

		if err != nil {
			m.log(fmt.Sprintf("%s tunnel stopped unexpectedly (%v), restarting in %s", cfg.Engine, err, delay))
		} else {
			m.log(fmt.Sprintf("%s tunnel process ended, restarting in %s", cfg.Engine, delay))
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}

		if delay < 60*time.Second {
			delay *= 2
			if delay > 60*time.Second {
				delay = 60 * time.Second
			}
		}
	}
}

// stopSupervisor terminates the running engine process and invalidates the
// supervisor goroutine that keeps it alive.
func (m *Manager) stopSupervisor() {
	m.mu.Lock()
	m.generation++
	cancel := m.cmdCancel
	m.cmdCancel = nil
	cmd := m.cmd
	m.cmd = nil
	m.active = false
	m.publicURL = ""
	m.ipAddress = ""
	m.activeEngine = ""
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// Shutdown stops the engine process without changing the persisted state, so a
// running tunnel is restored after the daemon is restarted.
func (m *Manager) Shutdown() {
	m.stopSupervisor()
}

// StopTunnel stops the engine process and disables the tunnel so it is not
// started again after a reboot.
func (m *Manager) StopTunnel() error {
	m.stopSupervisor()

	m.mu.RLock()
	cfg := m.config
	m.mu.RUnlock()
	cfg.Enabled = false
	if err := m.SaveConfig(&cfg); err != nil {
		logger.Get().Warnf("Tunnel", "Failed persisting disabled tunnel state: %v", err)
	}

	m.log("Tunnel service stopped")
	logger.Get().Infof("Tunnel", "Tunnel service stopped")
	return nil
}

// StartTunnel starts the configured engine. The process is supervised, so it
// is restarted automatically when it dies.
func (m *Manager) StartTunnel(cfg TunnelConfig) error {
	if strings.TrimSpace(cfg.Engine) == "" {
		cfg.Engine = "cloudflare"
	}

	binPath := FindBinary(cfg.Engine)
	if binPath == "" {
		m.log(fmt.Sprintf("Error: binary for engine %s not found", cfg.Engine))
		return fmt.Errorf("binary for engine %s not found", cfg.Engine)
	}

	// Replace the previous run with the new one.
	m.stopSupervisor()

	switch strings.ToLower(cfg.Engine) {
	case "tailscale":
		if err := m.startTailscale(cfg, binPath); err != nil {
			return err
		}
	case "zerotier":
		if err := m.startZeroTier(cfg, binPath); err != nil {
			return err
		}
	default:
		ctx, cancel := context.WithCancel(context.Background())

		m.mu.Lock()
		m.generation++
		gen := m.generation
		m.cmdCancel = cancel
		m.activeEngine = cfg.Engine
		// The engine is launched by the supervisor, but the tunnel is already
		// starting at this point.
		m.active = true
		m.mu.Unlock()

		go m.supervise(ctx, gen, cfg, binPath)
	}

	// A running tunnel is enabled, so it is restored after a reboot.
	cfg.Enabled = true
	if err := m.SaveConfig(&cfg); err != nil {
		return err
	}

	logger.Get().Infof("Tunnel", "Tunnel started successfully using engine %s", cfg.Engine)
	return nil
}

func (m *Manager) startTailscale(cfg TunnelConfig, binPath string) error {
	var args []string
	if cfg.TailscaleAuthKey != "" {
		args = []string{"up", "--authkey", cfg.TailscaleAuthKey}
	} else {
		args = []string{"up"}
	}

	out, err := config.ExecSuTimeout(15*time.Second, fmt.Sprintf("%s %s", binPath, strings.Join(args, " ")))
	if err != nil {
		return fmt.Errorf("failed starting Tailscale: %v, output: %s", err, strings.TrimSpace(string(out)))
	}

	m.mu.Lock()
	m.active = true
	m.activeEngine = cfg.Engine
	m.appendLog("Tailscale connected successfully")
	m.mu.Unlock()

	// Query Tailscale IPv4 address
	ipOut, err := config.ExecSuTimeout(5*time.Second, fmt.Sprintf("%s ip -4", binPath))
	if err == nil {
		m.mu.Lock()
		m.ipAddress = strings.TrimSpace(string(ipOut))
		m.mu.Unlock()
	}

	return nil
}

func (m *Manager) startZeroTier(cfg TunnelConfig, binPath string) error {
	if cfg.ZeroTierNetworkID == "" {
		return fmt.Errorf("zerotier_network_id is required")
	}

	out, err := config.ExecSuTimeout(15*time.Second, fmt.Sprintf("%s join %s", binPath, cfg.ZeroTierNetworkID))
	if err != nil {
		return fmt.Errorf("failed joining ZeroTier network: %v, output: %s", err, strings.TrimSpace(string(out)))
	}

	m.mu.Lock()
	m.active = true
	m.activeEngine = cfg.Engine
	m.appendLog(fmt.Sprintf("ZeroTier joined network %s", cfg.ZeroTierNetworkID))
	m.mu.Unlock()

	// Query assigned ZeroTier IP
	listOut, err := config.ExecSuTimeout(5*time.Second, fmt.Sprintf("%s listnetworks", binPath))
	if err == nil {
		for f := range strings.FieldsSeq(string(listOut)) {
			if ip := net.ParseIP(strings.Split(f, "/")[0]); ip != nil {
				m.mu.Lock()
				m.ipAddress = ip.String()
				m.mu.Unlock()
				break
			}
		}
	}

	return nil
}

// AutoStartOnBoot restores the tunnel that was running before the last reboot.
func AutoStartOnBoot() {
	m := GetManager()
	cfg, err := m.LoadConfig()
	if err != nil || cfg == nil || !cfg.Enabled || cfg.Engine == "" {
		return
	}

	logger.Get().Infof("Tunnel", "Auto-starting %s tunnel from saved configuration", cfg.Engine)
	if err := m.StartTunnel(*cfg); err != nil {
		logger.Get().Warnf("Tunnel", "Auto-start failed: %v", err)
	}
}

func (m *Manager) GetStatus() TunnelStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	engine := m.config.Engine
	if engine == "" {
		engine = "cloudflare"
	}

	binPath := FindBinary(engine)

	logsCopy := make([]string, len(m.logs))
	copy(logsCopy, m.logs)

	return TunnelStatus{
		Engine:      engine,
		Active:      m.active,
		PublicURL:   m.publicURL,
		IPAddress:   m.ipAddress,
		Logs:        logsCopy,
		BinaryFound: binPath != "",
		BinaryPath:  binPath,
	}
}
