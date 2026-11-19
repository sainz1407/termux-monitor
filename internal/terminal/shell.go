package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"termux-monitor/internal/config"
	"termux-monitor/internal/store"
)

// defaultEnv is appended after os.Environ so the shell always sees a sane
// Android root userland regardless of how the daemon was started.
var defaultEnv = []string{
	"HOME=/data/adb",
	"TERM=xterm-256color",
	"COLORTERM=truecolor",
	"USER=root",
	"LOGNAME=root",
	"TMPDIR=/data/local/tmp",
	"SHELL=/system/bin/sh",
	"PATH=/data/adb/ap/bin:/data/adb/ksu/bin:/data/adb/magisk:/data/data/com.termux/files/usr/bin:/system/bin:/system/xbin:/vendor/bin:/apex/com.android.runtime/bin",
}

// busyboxCandidates are the common bundled busybox locations on rooted
// Android (Magisk, APatch, KernelSU, stock).
var busyboxCandidates = []string{
	"/data/adb/magisk/busybox",
	"/data/adb/apatch/busybox",
	"/data/adb/ksu/bin/busybox",
	"/system/bin/busybox",
	"/system/xbin/busybox",
}

// termuxPrefixes are the Termux userland roots the terminal can attach to.
// The shell runs as the Termux user because the Termux package manager
// refuses to run as root: `pkg` exits with "Cannot run 'pkg' command as
// root" otherwise.
var termuxPrefixes = []string{
	"/data/data/com.termux/files/usr",
}

// androidEnv holds the core Android variables init exports to every app
// process. The daemon inherits a minimal environment when it is launched from
// service.sh, so the Termux shell gets them explicitly to match a real Termux
// session. Version dependent values (/apex roots, BOOTCLASSPATH) are inherited
// from the daemon instead of guessed here.
var androidEnv = []string{
	"ANDROID_ROOT=/system",
	"ANDROID_DATA=/data",
	"ANDROID_STORAGE=/storage",
	"EXTERNAL_STORAGE=/storage/emulated/0",
}

// ShellSpec describes one shell candidate for the terminal session.
type ShellSpec struct {
	// Argv is the command line to execute.
	Argv []string
	// UID is the user the shell runs as. Zero keeps the daemon's own user.
	UID uint32
	// Groups is the supplementary group list applied together with UID. It
	// must carry AID_INET so the non-root shell can open sockets; see
	// credentialAttr.
	Groups []int
	// Env is the complete environment handed to the shell.
	Env []string
	// Dir is the working directory of the shell.
	Dir string
	// Label is the human readable shell name shown in the session banner.
	Label string
}

// termuxProfile describes an installed Termux userland.
type termuxProfile struct {
	uid    uint32
	prefix string
	home   string
	tmp    string
}

func getTerminalWorkingDir() string {
	candidates := []string{"/data/adb", "/data/local/tmp", "/sdcard"}
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	if currentDir, err := os.Getwd(); err == nil && currentDir != "" {
		return currentDir
	}
	return os.TempDir()
}

// ConfigureTerminalCmd configures working directory and default environment for shell execution.
func ConfigureTerminalCmd(cmd *exec.Cmd) {
	cmd.Dir = getTerminalWorkingDir()
	cmd.Env = append(os.Environ(), defaultEnv...)
}

// androidUserName renders an Android app uid the way the platform does
// (10171 becomes u0_a171).
func androidUserName(uid uint32) string {
	if uid < 10000 {
		return fmt.Sprintf("u%d", uid)
	}
	appID := uid % 100000
	return fmt.Sprintf("u%d_a%d", uid/100000, appID%10000)
}

// isDir reports whether path exists and is a directory.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// detectTermux returns the installed Termux userland, or nil when Termux is
// missing or the daemon may not switch users.
func detectTermux() *termuxProfile {
	if os.Geteuid() != 0 {
		return nil
	}
	for _, prefix := range termuxPrefixes {
		bash := filepath.Join(prefix, "bin", "bash")
		if info, err := os.Stat(bash); err != nil || info.IsDir() {
			continue
		}
		info, err := os.Stat(prefix)
		if err != nil {
			continue
		}
		uid, ok := fileOwnerUID(info)
		if !ok || uid == 0 {
			continue
		}
		files := filepath.Dir(prefix)
		profile := &termuxProfile{
			uid:    uid,
			prefix: prefix,
			home:   filepath.Join(files, "home"),
			tmp:    filepath.Join(prefix, "tmp"),
		}
		// Real Termux sets TMPDIR to $PREFIX/tmp. Fall back to the legacy
		// files/tmp location and finally to a world writable directory, since
		// the shell runs as the Termux user and cannot write /data/local/tmp
		// on every device.
		if !isDir(profile.tmp) {
			profile.tmp = filepath.Join(files, "tmp")
		}
		if !isDir(profile.tmp) {
			profile.tmp = "/data/local/tmp"
		}
		return profile
	}
	return nil
}

// termuxExecPreload returns the termux-exec preload library shipped by the
// installed userland, or "" when the package is absent. Newer Termux splits
// the shell-command compatibility layer into its own library, while older
// releases shipped a single one; probing keeps both working. A wrong
// LD_PRELOAD path breaks every command, so the file is verified first.
func termuxExecPreload(prefix string) string {
	for _, name := range []string{"libtermux-exec-ld-preload.so", "libtermux-exec.so"} {
		path := filepath.Join(prefix, "lib", name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

// termuxEnv builds the environment the Termux app itself hands to its
// shells: the prefix variables make `pkg`, `apt` and the other Termux tools
// work, and the library path resolves the Termux shared objects.
//
// os.Environ comes first so the daemon's PATH or HOME can never shadow the
// Termux values: exec passes the last value of a duplicated key to the child.
// The Android core variables are added because the daemon runs with a minimal
// environment, and termux-exec is preloaded when present so shell scripts see
// the Termux filesystem layout like they do in the app.
func termuxEnv(profile *termuxProfile) []string {
	env := append(os.Environ(),
		"HOME="+profile.home,
		"PREFIX="+profile.prefix,
		"PATH="+profile.prefix+"/bin:/system/bin:/sbin",
		"LD_LIBRARY_PATH="+profile.prefix+"/lib",
		"TMPDIR="+profile.tmp,
		"SHELL="+filepath.Join(profile.prefix, "bin", "bash"),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"USER="+androidUserName(profile.uid),
		"LOGNAME="+androidUserName(profile.uid),
	)
	env = append(env, androidEnv...)
	if preload := termuxExecPreload(profile.prefix); preload != "" {
		env = append(env, "LD_PRELOAD="+preload)
	}
	return env
}

// shellRecipe builds the command for a shell candidate. A candidate with a
// UID runs the shell as that user, which keeps the controlling terminal and
// job control intact because the pty is allocated before the privilege drop.
// The supplementary groups travel with the credential so the dropped shell
// keeps AID_INET and can still open network sockets.
func shellRecipe(spec ShellSpec) *exec.Cmd {
	cmd := exec.Command(spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	if spec.UID != 0 {
		cmd.SysProcAttr = credentialAttr(spec.UID, spec.Groups)
	}
	return cmd
}

// shellCandidates returns the shell commands to try, in order:
//  1. the Termux userland shell, so `pkg` and the other Termux tools work
//  2. config.SUBin (su, or sh when the daemon already runs as uid 0)
//  3. every bundled busybox `sh`
//  4. /system/bin/sh as the last resort
func shellCandidates() []ShellSpec {
	rootEnv := append(os.Environ(), defaultEnv...)
	rootDir := getTerminalWorkingDir()

	var specs []ShellSpec
	if profile := detectTermux(); profile != nil {
		specs = append(specs, ShellSpec{
			Argv:   []string{filepath.Join(profile.prefix, "bin", "bash"), "-i"},
			UID:    profile.uid,
			Groups: store.TermuxGroups(int(profile.uid)),
			Env:    termuxEnv(profile),
			Dir:    profile.home,
			Label:  "termux bash",
		})
	}

	specs = append(specs, ShellSpec{
		Argv:  []string{config.SUBin},
		Env:   rootEnv,
		Dir:   rootDir,
		Label: filepath.Base(config.SUBin),
	})
	for _, bb := range busyboxCandidates {
		if _, err := os.Stat(bb); err == nil {
			specs = append(specs, ShellSpec{
				Argv:  []string{bb, "sh"},
				Env:   rootEnv,
				Dir:   rootDir,
				Label: "busybox sh",
			})
		}
	}
	specs = append(specs, ShellSpec{
		Argv:  []string{"/system/bin/sh"},
		Env:   rootEnv,
		Dir:   rootDir,
		Label: "sh",
	})
	return specs
}
