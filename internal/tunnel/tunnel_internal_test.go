package tunnel

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBuildArgsCloudflare(t *testing.T) {
	SetLocalPort(8080)
	defer SetLocalPort(80)

	args, err := buildArgs(TunnelConfig{Engine: "cloudflare", CloudflareQuick: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--url http://localhost:8080") {
		t.Errorf("expected the local origin in the arguments, got %q", joined)
	}
	if !strings.Contains(joined, "--no-autoupdate") {
		t.Errorf("expected --no-autoupdate in the arguments, got %q", joined)
	}

	args, err = buildArgs(TunnelConfig{Engine: "cloudflare", CloudflareToken: "secret-token"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined = strings.Join(args, " ")
	if !strings.Contains(joined, "run --token secret-token") {
		t.Errorf("expected a named tunnel command, got %q", joined)
	}
}

func TestBuildArgsRejectsUnsupervisedEngine(t *testing.T) {
	if _, err := buildArgs(TunnelConfig{Engine: "tailscale"}); err == nil {
		t.Error("expected an error for an engine that cannot run as a supervised process")
	}
}

func TestLocalOriginFollowsConfiguredPort(t *testing.T) {
	SetLocalPort(9090)
	defer SetLocalPort(80)

	if origin := localOrigin(); origin != "http://localhost:9090" {
		t.Errorf("unexpected local origin: %s", origin)
	}
}

func TestInterestingLine(t *testing.T) {
	cases := map[string]bool{
		"ERR Failed to fetch features":              true,
		"INF Registered tunnel connection connIndex": true,
		"INF Starting metrics server":               false,
		"INF Your quick Tunnel has been created":    true,
	}
	for line, expected := range cases {
		if got := interestingLine(line); got != expected {
			t.Errorf("interestingLine(%q) = %v, want %v", line, got, expected)
		}
	}
}

// TestSupervisorRestartsCrashedEngine verifies that a tunnel engine that dies
// on its own is started again by the supervisor.
func TestSupervisorRestartsCrashedEngine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the mock engine is a POSIX shell script")
	}

	tempDir := t.TempDir()
	t.Setenv("TMON_DATA_DIR", tempDir)

	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("failed creating bin dir: %v", err)
	}
	mock := filepath.Join(binDir, "cloudflared")
	script := "#!/bin/sh\necho 'ERR mock engine stopped'\nexit 1\n"
	if err := os.WriteFile(mock, []byte(script), 0755); err != nil {
		t.Fatalf("failed writing the mock engine: %v", err)
	}

	originalDelay := restartBaseDelay
	restartBaseDelay = 100 * time.Millisecond
	defer func() { restartBaseDelay = originalDelay }()

	mgr := newManager()
	if err := mgr.StartTunnel(TunnelConfig{Engine: "cloudflare", CloudflareQuick: true}); err != nil {
		t.Fatalf("failed starting the tunnel: %v", err)
	}
	defer mgr.StopTunnel()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(strings.Join(mgr.GetStatus().Logs, "\n"), "restarting in") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("expected the supervisor to restart the engine, logs: %v", mgr.GetStatus().Logs)
}

// TestStopTunnelDisablesPersistedConfig makes sure a stopped tunnel is not
// resurrected after a reboot.
func TestStopTunnelDisablesPersistedConfig(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMON_DATA_DIR", tempDir)

	mgr := newManager()
	if err := mgr.SaveConfig(&TunnelConfig{Engine: "cloudflare", Enabled: true}); err != nil {
		t.Fatalf("failed saving config: %v", err)
	}

	if err := mgr.StopTunnel(); err != nil {
		t.Fatalf("unexpected error stopping tunnel: %v", err)
	}

	loaded, err := mgr.LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}
	if loaded.Enabled {
		t.Error("expected the persisted tunnel to be disabled after stopping")
	}
}

// processAlive reports whether a process still exists.
func processAlive(pid int) bool {
	_, err := os.Stat("/proc/" + strconv.Itoa(pid))
	return err == nil
}

// TestShutdownStopsEngineProcess verifies that the daemon shutdown also stops
// the tunnel engine, so it does not survive as an orphan process.
func TestShutdownStopsEngineProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the mock engine is a POSIX shell script")
	}

	tempDir := t.TempDir()
	t.Setenv("TMON_DATA_DIR", tempDir)

	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("failed creating bin dir: %v", err)
	}
	mock := filepath.Join(binDir, "cloudflared")
	script := "#!/bin/sh\nsleep 300\n"
	if err := os.WriteFile(mock, []byte(script), 0755); err != nil {
		t.Fatalf("failed writing the mock engine: %v", err)
	}

	mgr := newManager()
	if err := mgr.StartTunnel(TunnelConfig{Engine: "cloudflare", CloudflareQuick: true}); err != nil {
		t.Fatalf("failed starting the tunnel: %v", err)
	}

	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mgr.mu.RLock()
		cmd := mgr.cmd
		mgr.mu.RUnlock()
		if cmd != nil && cmd.Process != nil {
			pid = cmd.Process.Pid
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pid == 0 {
		mgr.Shutdown()
		t.Fatal("expected a running engine process")
	}

	mgr.Shutdown()

	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("expected the engine process %d to be stopped on shutdown", pid)
}

// TestCloudflaredEndToEnd starts a real quick tunnel, waits for the public URL
// and verifies that a killed engine process is restarted by the supervisor.
func TestCloudflaredEndToEnd(t *testing.T) {
	if os.Getenv("TMON_TUNNEL_E2E") != "1" {
		t.Skip("set TMON_TUNNEL_E2E=1 to run the live tunnel test")
	}

	tempDir := t.TempDir()
	t.Setenv("TMON_DATA_DIR", tempDir)

	if FindBinary("cloudflare") == "" {
		t.Skip("no cloudflared binary available")
	}

	originalDelay := restartBaseDelay
	restartBaseDelay = 5 * time.Second
	defer func() { restartBaseDelay = originalDelay }()

	mgr := newManager()
	if err := mgr.StartTunnel(TunnelConfig{Engine: "cloudflare", CloudflareQuick: true}); err != nil {
		t.Fatalf("failed starting the tunnel: %v", err)
	}
	defer mgr.StopTunnel()

	var firstURL string
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		firstURL = mgr.GetStatus().PublicURL
		if firstURL != "" {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if firstURL == "" {
		t.Fatalf("expected a public URL, logs: %v", mgr.GetStatus().Logs)
	}
	if !strings.Contains(firstURL, "trycloudflare.com") {
		t.Fatalf("unexpected public URL: %s", firstURL)
	}
	t.Logf("public URL: %s", firstURL)

	// Kill the engine process and wait for the supervisor to bring it back.
	mgr.mu.RLock()
	cmd := mgr.cmd
	mgr.mu.RUnlock()
	if cmd == nil || cmd.Process == nil {
		t.Fatal("expected a running engine process")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("failed killing the engine process: %v", err)
	}

	deadline = time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.GetStatus().PublicURL != "" && mgr.GetStatus().PublicURL != firstURL {
			t.Logf("restarted with a new public URL: %s", mgr.GetStatus().PublicURL)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("expected the supervisor to restart the engine, logs: %v", mgr.GetStatus().Logs)
}
