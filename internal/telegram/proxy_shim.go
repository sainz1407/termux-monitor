package telegram

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"termux-monitor/internal/config"
)

// proxyHTTPClient is a short-timeout client for the local Clash REST API probe.
var proxyHTTPClient = &http.Client{Timeout: 5 * time.Second}

// proxy_shim.go keeps the Telegram bot's `/proxy` command functional after the
// Box-for-Magisk (BFM) integration was removed from the HTTP/API surface. It is
// intentionally self-contained and does not depend on the former
// internal/proxy package: it only probes generic core binaries (mihomo/clash)
// via /proc and, when a BFM-style service script is present, drives it with a
// plain shell command.

// proxyCoreInfo mirrors the fields the Telegram message needs.
type proxyCoreInfo struct {
	Name    string
	Path    string
	Exists  bool
	Running bool
	PID     int
	Memory  string
}

// proxyBoxBasePath returns the BFM base directory.
func proxyBoxBasePath() string {
	if v := os.Getenv("TMON_BOX_BASE"); v != "" {
		return v
	}
	return "/data/adb/box"
}

// proxyClashBasePath returns the legacy Clash base directory.
func proxyClashBasePath() string {
	if v := os.Getenv("TMON_CLASH_BASE"); v != "" {
		return v
	}
	return "/data/adb/clash"
}

// proxyCandidateCorePaths enumerates the well-known core binary locations.
func proxyCandidateCorePaths() []string {
	boxPath := proxyBoxBasePath()
	clashPath := proxyClashBasePath()
	paths := []string{
		boxPath + "/bin/mihomo",
		clashPath + "/clash",
		"/data/adb/modules/box4magisk/bin/mihomo",
		"/data/adb/modules/clash_for_magisk/bin/clash",
		"/system/bin/mihomo",
		"/system/bin/clash",
	}
	seen := make(map[string]bool, len(paths))
	for _, p := range paths {
		seen[p] = true
	}
	for _, coreName := range []string{"mihomo", "clash"} {
		if path, err := exec.LookPath(coreName); err == nil && path != "" && !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	return paths
}

// proxyFindPID scans /proc for a process whose comm or cmdline matches name.
func proxyFindPID(name string) (int, bool) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, false
	}
	nameLower := strings.ToLower(name)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		if comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid)); err == nil {
			commStr := strings.TrimSpace(string(comm))
			if strings.EqualFold(commStr, name) || strings.Contains(strings.ToLower(commStr), nameLower) {
				return pid, true
			}
		}
		if cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil && len(cmdline) > 0 {
			cmdStr := strings.TrimSpace(strings.ReplaceAll(string(cmdline), "\x00", " "))
			fields := strings.Fields(cmdStr)
			if len(fields) > 0 {
				base := filepath.Base(fields[0])
				if strings.EqualFold(base, name) || strings.Contains(strings.ToLower(cmdStr), nameLower) {
					return pid, true
				}
			}
		}
	}
	return 0, false
}

// proxyCheckRunning reports the PID of a running core, falling back to pidof.
func proxyCheckRunning(name string) (int, bool) {
	if pid, ok := proxyFindPID(name); ok {
		return pid, true
	}
	out, err := exec.Command(config.SUBin, "-c", "pidof "+name).Output()
	if err == nil {
		if fields := strings.Fields(string(out)); len(fields) > 0 {
			if pid, err := strconv.Atoi(fields[0]); err == nil {
				return pid, true
			}
		}
	}
	return 0, false
}

// proxyMemoryUsage reads VmRSS from /proc/<pid>/status.
func proxyMemoryUsage(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return "N/A"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			return strings.TrimSpace(line)
		}
	}
	return "N/A"
}

// proxyDetectCores lists the candidate cores with their live state.
func proxyDetectCores() []proxyCoreInfo {
	var list []proxyCoreInfo
	seen := make(map[string]bool)
	for _, p := range proxyCandidateCorePaths() {
		if seen[p] {
			continue
		}
		seen[p] = true
		name := "mihomo"
		if strings.Contains(strings.ToLower(p), "clash") {
			name = "clash"
		}
		info := proxyCoreInfo{Name: name, Path: p}
		if _, err := os.Stat(p); err == nil {
			info.Exists = true
			pid, running := proxyCheckRunning(name)
			info.Running = running
			info.PID = pid
			if running && pid > 0 {
				info.Memory = proxyMemoryUsage(pid)
			}
		}
		list = append(list, info)
	}
	return list
}

// proxyGetMode reports the active routing mode. Without a reachable Clash REST
// API the mode is unknown, so it falls back to the neutral "Rule" default.
func proxyGetMode() string {
	apiBase := strings.TrimRight(config.ClashAPI, "/")
	resp, err := proxyHTTPClient.Get(apiBase + "/configs")
	if err != nil {
		return "Rule"
	}
	defer resp.Body.Close()
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	switch {
	case strings.Contains(body, `"mode":"Global"`):
		return "Global"
	case strings.Contains(body, `"mode":"Direct"`):
		return "Direct"
	default:
		return "Rule"
	}
}

// proxyWatchdogState reports whether a watchdog supervisor is currently armed.
// The legacy in-memory flag lived in internal/proxy; with that package removed
// there is no supervisor to report, so this is always false.
func proxyWatchdogState() bool { return false }

// proxyControlService drives the BFM service script when present, otherwise it
// performs a best-effort start/stop/restart of a bare core binary.
func proxyControlService(action string) error {
	boxPath := proxyBoxBasePath()
	clashPath := proxyClashBasePath()

	var cmdStr string
	switch action {
	case "start":
		if _, ok := proxyCheckRunning("mihomo"); ok {
			return nil
		}
		if _, ok := proxyCheckRunning("clash"); ok {
			return nil
		}
		cmdStr = fmt.Sprintf(
			"if [ -f %s/scripts/box.service ]; then %s/scripts/box.service start; elif [ -f %s/scripts/clash.service ]; then %s/scripts/clash.service start; else %s -c mihomo -d %s/bin/ & fi",
			boxPath, boxPath, clashPath, clashPath, config.SUBin, boxPath,
		)
	case "stop":
		cmdStr = fmt.Sprintf(
			"if [ -f %s/scripts/box.service ]; then %s/scripts/box.service stop; elif [ -f %s/scripts/clash.service ]; then %s/scripts/clash.service stop; else killall mihomo clash 2>/dev/null || true; fi",
			boxPath, boxPath, clashPath, clashPath,
		)
	case "restart":
		cmdStr = fmt.Sprintf(
			"if [ -f %s/scripts/box.service ]; then %s/scripts/box.service restart; elif [ -f %s/scripts/clash.service ]; then %s/scripts/clash.service restart; else killall mihomo clash 2>/dev/null; sleep 1; mihomo -d %s/bin/ & fi",
			boxPath, boxPath, clashPath, clashPath, boxPath,
		)
	default:
		return fmt.Errorf("unknown action: %s", action)
	}

	out, err := exec.Command(config.SUBin, "-c", cmdStr).CombinedOutput()
	if err != nil {
		return fmt.Errorf("control error: %v, out: %s", err, string(out))
	}
	return nil
}
