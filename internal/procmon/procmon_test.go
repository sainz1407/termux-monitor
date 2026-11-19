package procmon

import (
	"os"
	"runtime"
	"testing"
)

func TestParseProcStat(t *testing.T) {
	raw := "1234 (my proc (name)) S 1 1234 1234 0 -1 4194560 1000 0 0 0 " +
		"250 120 0 0 20 0 8 0 500 123456789 4096 18446744073709551615 1 2 3"

	name, fields := parseProcStat(raw)
	if name != "my proc (name)" {
		t.Errorf("unexpected process name: %q", name)
	}
	if len(fields) < 22 {
		t.Fatalf("expected at least 22 fields, got %d", len(fields))
	}
	if fields[0] != "S" {
		t.Errorf("unexpected state field: %q", fields[0])
	}
	if fields[1] != "1" {
		t.Errorf("unexpected parent pid: %q", fields[1])
	}
	if fields[11] != "250" || fields[12] != "120" {
		t.Errorf("unexpected tick fields: %q %q", fields[11], fields[12])
	}
	if fields[19] != "500" {
		t.Errorf("unexpected start time: %q", fields[19])
	}
	if fields[21] != "4096" {
		t.Errorf("unexpected rss: %q", fields[21])
	}
}

func TestStateName(t *testing.T) {
	cases := map[string]string{
		"R": "running",
		"S": "sleeping",
		"D": "disk-sleep",
		"Z": "zombie",
		"T": "stopped",
	}
	for state, expected := range cases {
		if got := stateName(state); got != expected {
			t.Errorf("stateName(%q) = %q, want %q", state, got, expected)
		}
	}
}

func TestRuntimeKind(t *testing.T) {
	cases := map[string]string{
		"python":        "Python",
		"python3":       "Python",
		"python3.13":    "Python",
		"node":          "Node.js",
		"node22":        "Node.js",
		"php":           "PHP",
		"php8.2":        "PHP",
		"sh":            "Shell",
		"bash":          "Shell",
		"java":          "Java",
		"app_process32": "Java",
		"ruby":          "Ruby",
		"lua":           "Lua",
	}
	for base, expected := range cases {
		kind, ok := runtimeKind(base)
		if !ok {
			t.Errorf("runtimeKind(%q) found no runtime", base)
			continue
		}
		if kind != expected {
			t.Errorf("runtimeKind(%q) = %q, want %q", base, kind, expected)
		}
	}

	if _, ok := runtimeKind("some-unknown-binary"); ok {
		t.Error("expected no runtime for an unknown binary")
	}
}

func TestUserName(t *testing.T) {
	if got := userName(0); got != "root" {
		t.Errorf("userName(0) = %q, want root", got)
	}
	if got := userName(10123); got != "u0_a123" {
		t.Errorf("userName(10123) = %q, want u0_a123", got)
	}
	if got := userName(1010123); got != "u10_a123" {
		t.Errorf("userName(1010123) = %q, want u10_a123", got)
	}
	if got := userName(-1); got != "unknown" {
		t.Errorf("userName(-1) = %q, want unknown", got)
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{
		512:             "512 B",
		2048:            "2.0 KB",
		5 * 1024 * 1024: "5.0 MB",
	}
	for value, expected := range cases {
		if got := formatBytes(value); got != expected {
			t.Errorf("formatBytes(%d) = %q, want %q", value, got, expected)
		}
	}
}

func TestFormatElapsed(t *testing.T) {
	cases := map[float64]string{
		45:     "45s",
		125:    "2m 5s",
		7300:   "2h 1m",
		180000: "2d 2h",
	}
	for value, expected := range cases {
		if got := formatElapsed(value); got != expected {
			t.Errorf("formatElapsed(%v) = %q, want %q", value, got, expected)
		}
	}
}

// TestListReadsProc verifies that the process list contains the test process
// itself and that the search filter narrows the result down.
func TestListReadsProc(t *testing.T) {
	if _, err := os.Stat("/proc/self/stat"); err != nil {
		t.Skip("the proc filesystem is not available on this platform")
	}

	mon := NewMonitor()
	procs := mon.List("", "cpu", 0)
	if len(procs) == 0 {
		t.Fatal("expected at least one process")
	}

	found := false
	for _, proc := range procs {
		if proc.PID == os.Getpid() {
			found = true
			if proc.Name == "" {
				t.Error("expected a process name")
			}
			if proc.Runtime == "" {
				t.Error("expected a runtime label")
			}
			if proc.User == "" {
				t.Error("expected a user name")
			}
		}
	}
	if !found {
		t.Error("expected the test process in the process list")
	}

	filtered := mon.List("this-name-does-not-exist", "cpu", 0)
	if len(filtered) != 0 {
		t.Errorf("expected an empty result for a missing search term, got %d", len(filtered))
	}
}

// TestIsGoBinary uses the test binary itself, it is always built with the Go
// toolchain.
func TestIsGoBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Go detection reads the ELF sections")
	}

	exe, err := os.Executable()
	if err != nil {
		t.Skipf("failed locating the test binary: %v", err)
	}
	if !isGoBinary(exe) {
		t.Error("expected the test binary to be detected as a Go binary")
	}
}

// TestKillRefusesProtectedProcesses makes sure the monitor never kills the
// init process, itself or its own parent.
func TestKillRefusesProtectedProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process signalling is not available on Windows")
	}

	mon := NewMonitor()
	if err := mon.Kill(1, true); err == nil {
		t.Error("expected killing PID 1 to be refused")
	}
	if err := mon.Kill(os.Getpid(), true); err == nil {
		t.Error("expected killing the monitor itself to be refused")
	}
	if err := mon.Kill(os.Getppid(), true); err == nil {
		t.Error("expected killing the parent process to be refused")
	}
}
