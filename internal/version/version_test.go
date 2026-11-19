package version

import (
	"os"
	"path/filepath"
	"testing"

	"termux-monitor/internal/config"
)

func TestParseUsesModulePropValues(t *testing.T) {
	raw := "id=termux_monitor\nversion=v9.9.9\nversionCode=999\nauthor=x\n"
	version, code := parse(raw)
	if version != "v9.9.9" || code != "999" {
		t.Fatalf("parse = %q, %q; want v9.9.9, 999", version, code)
	}
}

func TestParseFallsBackOnMissingFields(t *testing.T) {
	version, code := parse("id=termux_monitor\n")
	if version != FallbackVersion || code != FallbackVersionCode {
		t.Fatalf("parse = %q, %q; want %q, %q", version, code, FallbackVersion, FallbackVersionCode)
	}
}

// The installed module.prop must win over the compiled fallback, otherwise the
// UI keeps reporting an old version after a module update.
func TestResolveReadsModuleProp(t *testing.T) {
	dir := t.TempDir()
	prop := filepath.Join(dir, "module.prop")
	if err := os.WriteFile(prop, []byte("id=termux_monitor\nversion=v2.4.6\nversionCode=246\n"), 0644); err != nil {
		t.Fatalf("write module.prop: %v", err)
	}

	original := config.ModuleDir
	config.ModuleDir = dir
	defer func() { config.ModuleDir = original }()

	if got := resolve(); got != "v2.4.6 (246)" {
		t.Fatalf("resolve = %q, want v2.4.6 (246)", got)
	}
}

func TestResolveFallsBackWhenModulePropIsMissing(t *testing.T) {
	original := config.ModuleDir
	config.ModuleDir = t.TempDir()
	defer func() { config.ModuleDir = original }()

	want := FallbackVersion + " (" + FallbackVersionCode + ")"
	if got := resolve(); got != want {
		t.Fatalf("resolve = %q, want %q", got, want)
	}
}
