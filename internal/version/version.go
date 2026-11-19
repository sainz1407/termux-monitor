package version

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"termux-monitor/internal/config"
)

// Fallback is used when module.prop cannot be read, for example when the
// daemon runs outside of an installed module directory.
const (
	FallbackVersion     = "v1.3.0"
	FallbackVersionCode = "130"
)

var (
	once      sync.Once
	resolved  string
	versionRe = regexp.MustCompile(`(?m)^version=(.+)$`)
	codeRe    = regexp.MustCompile(`(?m)^versionCode=(.+)$`)
)

// modulePropPaths lists the locations module.prop can live in.
func modulePropPaths() []string {
	paths := []string{filepath.Join(config.ModuleDir, "module.prop")}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		paths = append(paths,
			filepath.Join(dir, "module.prop"),
			filepath.Join(dir, "..", "module.prop"),
		)
	}
	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(cwd, "module.prop"))
	}
	return paths
}

// parse extracts version and versionCode from a module.prop payload.
func parse(raw string) (string, string) {
	version, code := FallbackVersion, FallbackVersionCode
	if match := versionRe.FindStringSubmatch(raw); match != nil {
		if value := strings.TrimSpace(match[1]); value != "" {
			version = value
		}
	}
	if match := codeRe.FindStringSubmatch(raw); match != nil {
		if value := strings.TrimSpace(match[1]); value != "" {
			code = value
		}
	}
	return version, code
}

// Current returns the module version in the "vX.Y.Z (CODE)" format shown in
// the UI. module.prop is the single source of truth for the version, so the
// daemon always reports the version that is actually installed on the device.
func Current() string {
	once.Do(func() {
		resolved = resolve()
	})
	return resolved
}

// resolve looks up module.prop and formats the version for display.
func resolve() string {
	version, code := FallbackVersion, FallbackVersionCode
	for _, path := range modulePropPaths() {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		version, code = parse(string(raw))
		break
	}
	return version + " (" + code + ")"
}
