package procmon

import (
	"debug/elf"
	"strings"
)

// runtimeKindByExe maps the base name of an executable to the runtime it
// provides.
var runtimeKindByExe = map[string]string{
	"python":      "Python",
	"python2":     "Python",
	"python3":     "Python",
	"pypy":        "PyPy",
	"pypy3":       "PyPy",
	"node":        "Node.js",
	"nodejs":      "Node.js",
	"deno":        "Deno",
	"bun":         "Bun",
	"java":        "Java",
	"javaw":       "Java",
	"php":         "PHP",
	"php-cgi":     "PHP",
	"ruby":        "Ruby",
	"perl":        "Perl",
	"lua":         "Lua",
	"luajit":      "Lua",
	"dotnet":      ".NET",
	"mono":        ".NET",
	"wine":        "Wine",
	"R":           "R",
	"r":           "R",
	"sh":          "Shell",
	"bash":        "Shell",
	"ash":         "Shell",
	"dash":        "Shell",
	"zsh":         "Shell",
	"ksh":         "Shell",
	"ksh93":       "Shell",
	"fish":        "Shell",
	"busybox":     "Shell",
	"toybox":      "Shell",
	"termux-exec": "Termux",
	"sshd":        "OpenSSH",
}

// runtimeKindByPrefix matches the versioned names of the interpreters, such
// as python3.13 or node22.
var runtimeKindByPrefix = []struct {
	prefix string
	kind   string
}{
	{"python", "Python"},
	{"pypy", "PyPy"},
	{"node", "Node.js"},
	{"php", "PHP"},
	{"ruby", "Ruby"},
	{"perl", "Perl"},
	{"luajit", "Lua"},
	{"lua", "Lua"},
	{"dotnet", ".NET"},
	{"javaw", "Java"},
	{"java", "Java"},
	{"app_process", "Java"},
	{"dalvikvm", "Java"},
	{"zygote", "Java"},
}

// detectRuntime identifies the runtime or interpreter behind a process.
func (m *Monitor) detectRuntime(pid int, name, cmdline string) string {
	path := exePath(pid)

	// Kernel threads have neither an executable nor a command line.
	if path == "" && cmdline == "" {
		return "Kernel"
	}
	if strings.HasPrefix(name, "[") && strings.HasSuffix(name, "]") {
		return "Kernel"
	}

	if path != "" {
		if kind, ok := m.runtimeForExe(path); ok {
			return kind
		}
	}

	// Fall back to the first argument of the command line.
	if fields := strings.Fields(cmdline); len(fields) > 0 {
		if kind, ok := runtimeKind(baseName(fields[0])); ok {
			return kind
		}
	}

	if path == "" {
		return "Unknown"
	}
	return "Native"
}

// runtimeForExe detects the runtime of an executable. The result is cached,
// because the detection reads the binary header.
func (m *Monitor) runtimeForExe(path string) (string, bool) {
	m.mu.Lock()
	cached, ok := m.exeRuntime[path]
	m.mu.Unlock()
	if ok {
		return cached, cached != ""
	}

	kind := ""
	if matched, found := runtimeKind(baseName(path)); found {
		kind = matched
	} else if isGoBinary(path) {
		kind = "Go"
	}

	m.mu.Lock()
	m.exeRuntime[path] = kind
	m.mu.Unlock()

	return kind, kind != ""
}

// runtimeKind matches the base name of an executable against the known
// runtimes.
func runtimeKind(base string) (string, bool) {
	if kind, ok := runtimeKindByExe[base]; ok {
		return kind, true
	}
	lower := strings.ToLower(base)
	for _, rule := range runtimeKindByPrefix {
		if strings.HasPrefix(lower, rule.prefix) {
			return rule.kind, true
		}
	}
	return "", false
}

// isGoBinary reports whether the executable was built with the Go toolchain.
func isGoBinary(path string) bool {
	file, err := elf.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	if file.Section(".go.buildinfo") != nil {
		return true
	}
	// Binaries built with an older toolchain keep the build information in the
	// symbol table instead of a dedicated section.
	symbols, err := file.Symbols()
	if err != nil {
		return false
	}
	for _, symbol := range symbols {
		if symbol.Name == "runtime.buildinfo" {
			return true
		}
	}
	return false
}
