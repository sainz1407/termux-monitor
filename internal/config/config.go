package config

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// envOrDefault returns env value or fallback if not set.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// TMON_SU_BIN override for root shell invocation.
var SUBin = func() string {
	defaultBin := "su"
	if os.Getuid() == 0 {
		defaultBin = "sh"
	}
	return envOrDefault("TMON_SU_BIN", defaultBin)
}()

// ExecSuContext executes a command string with root privileges using context cancellation.
func ExecSuContext(ctx context.Context, cmdStr string) ([]byte, error) {
	return exec.CommandContext(ctx, SUBin, "-c", cmdStr).CombinedOutput()
}

// ExecSuTimeout executes a command string with root privileges using a hard timeout duration.
func ExecSuTimeout(d time.Duration, cmdStr string) ([]byte, error) {
	if d <= 0 {
		d = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return ExecSuContext(ctx, cmdStr)
}

// TMON_MODULE_DIR override for Magisk module installation directory.
var ModuleDir = envOrDefault("TMON_MODULE_DIR", "/data/adb/modules/termux_monitor")

// TMON_CLASH_API override for Clash controller REST API.
var ClashAPI = envOrDefault("TMON_CLASH_API", "http://127.0.0.1:9090")

// TMON_SMS_DB override for SMS telephony database path.
var SMSDb = envOrDefault("TMON_SMS_DB", "")

// TMON_LEASES_FILE override for dnsmasq leases file.
var LeasesFile = envOrDefault("TMON_LEASES_FILE", "/data/misc/dhcp/dnsmasq.leases")

// TMON_ALLOWED_DIRS list (comma-separated) for FileManager base directory boundaries.
var AllowedDirs = func() []string {
	if val := os.Getenv("TMON_ALLOWED_DIRS"); val != "" {
		var parts []string
		for _, s := range strings.Split(val, ",") {
			parts = append(parts, strings.TrimSpace(s))
		}
		return parts
	}
	// Root "/" is permitted by default for full Android root file navigation;
	// boundary restrictions can still be customized via TMON_ALLOWED_DIRS.
	return []string{"/", "/sdcard", "/storage", "/data", "/data/adb", "/data/local/tmp", "/data/system"}
}()

// GetPersistentDataDir returns the target directory path for persistent application data.
func GetPersistentDataDir() string {
	if val := os.Getenv("TMON_DATA_DIR"); val != "" {
		_ = os.MkdirAll(val, 0755)
		return val
	}

	dir := "/data/adb/termux_monitor/data"
	if _, err := os.Stat("/data/adb"); err != nil || os.Geteuid() != 0 {
		if ModuleDir != "" {
			dir = filepath.Join(ModuleDir, "data")
		} else {
			dir = "data"
		}
	}

	_ = os.MkdirAll(dir, 0755)
	return dir
}

// GetPersistentFilePath returns the path for a named file in the persistent data dir
// and automatically migrates legacy config files if found.
func GetPersistentFilePath(filename string) string {
	targetDir := GetPersistentDataDir()
	targetPath := filepath.Join(targetDir, filename)

	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		legacyPaths := []string{
			filepath.Join(ModuleDir, filename),
			filename,
		}
		for _, legacyPath := range legacyPaths {
			if legacyPath == targetPath {
				continue
			}
			if data, err := os.ReadFile(legacyPath); err == nil && len(data) > 0 {
				_ = WriteFileAtomic(targetPath, data, 0644)
				break
			}
		}
	}

	return targetPath
}

// WriteFileAtomic writes data to a file atomically by writing to a temporary file in the same directory,
// syncing it to disk, and renaming it to the target filename to prevent corrupted files during sudden reboots.
func WriteFileAtomic(filename string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()

	cleanup := func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
	}

	if _, err := tmpFile.Write(data); err != nil {
		cleanup()
		return err
	}

	if err := tmpFile.Sync(); err != nil {
		cleanup()
		return err
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}

	if err := os.Chmod(tmpName, perm); err != nil {
		_ = os.Remove(tmpName)
		return err
	}

	return os.Rename(tmpName, filename)
}
