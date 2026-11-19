// Package store implements the Termux package catalog and installer for the
// Store tab. Every command runs as the Termux application user (com.termux)
// via `su <uid> -G ... -c`, because the server itself runs as root from a
// Magisk module and must never install packages into $PREFIX as root. The
// supplementary groups are required for the non-root identity to open network
// sockets on Android (see aidInet).
package store

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Termux runtime layout. These are fixed by the Termux application and are
// not configurable.
const (
	// TermuxPackage is the package name used for UID resolution.
	TermuxPackage = "com.termux"
	// TermuxHome is the HOME of the Termux user.
	TermuxHome = "/data/data/" + TermuxPackage + "/files/home"
	// TermuxPrefix is the $PREFIX of the Termux user.
	TermuxPrefix = "/data/data/" + TermuxPackage + "/files/usr"
	// TermuxBin is the bin directory of the Termux user.
	TermuxBin = TermuxPrefix + "/bin"
	// TermuxTmp is the TMPDIR of the Termux user.
	TermuxTmp = TermuxPrefix + "/tmp"
)

// LogRing is the maximum number of retained log lines per run.
const LogRing = 500

// ProbeTimeout bounds presence and version probes.
const ProbeTimeout = 8 * time.Second

// versionLineMax caps a displayed version line.
const versionLineMax = 120

// uidCache caches the resolved Termux UID for the process lifetime. The UID
// only changes when Termux is reinstalled, which also restarts the module.
var (
	uidMu    sync.Mutex
	uidValue int = -1
)

// suBin is the root shell used to drop privileges into the Termux user. It is
// deliberately separate from config.SUBin: this package always invokes it as
// `su <uid> -c <script>` to *change* identity, so it must be a real su even
// when the server itself already runs as root. config.SUBin intentionally
// degrades to `sh` for root processes (used by commands that only need the
// `<bin> -c <cmd>` shape), which would turn our call into the invalid
// `sh <uid> -c ...`.
//
// It is kept as a bare name, not an absolute path: on Magisk devices su lives
// only on /sbin (as a symlink to the magisk binary), while other root managers
// place it elsewhere, so PATH resolution is the portable choice.
var suBin = func() string {
	if v := os.Getenv("TMON_SU_BIN"); v != "" {
		return v
	}
	return "su"
}()

// buildEnv returns the environment slice that makes Termux binaries behave as
// they would inside a real Termux shell. Root does not inherit any of this.
func buildEnv() []string {
	return []string{
		"HOME=" + TermuxHome,
		"PREFIX=" + TermuxPrefix,
		"TMPDIR=" + TermuxTmp,
		"PATH=" + TermuxBin + ":" + TermuxBin + "/applets:" + TermuxPrefix + "/bin",
		"LANG=C.UTF-8",
		"TERM=xterm-256color",
	}
}

// ResolveUID returns the Android UID of the Termux application. It reads
// /data/system/packages.list first and falls back to `cmd package list
// packages -U`. The result is cached; -1 means Termux is not installed.
func ResolveUID() int {
	uidMu.Lock()
	defer uidMu.Unlock()
	if uidValue != -1 {
		return uidValue
	}
	if uid := parsePackagesList("/data/system/packages.list"); uid != -1 {
		uidValue = uid
		return uidValue
	}
	if uid := uidFromCmdPackage(); uid != -1 {
		uidValue = uid
		return uidValue
	}
	return -1
}

// parsePackagesList extracts the UID for com.termux from a packages.list file.
func parsePackagesList(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return -1
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || fields[0] != TermuxPackage {
			continue
		}
		if uid, err := strconv.Atoi(fields[1]); err == nil {
			return uid
		}
	}
	return -1
}

// uidFromCmdPackage is the fallback resolution via the package manager CLI.
func uidFromCmdPackage() int {
	ctx, cancel := context.WithTimeout(context.Background(), ProbeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "cmd", "package", "list", "packages", "-U", TermuxPackage).Output()
	if err != nil {
		return -1
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "package:"+TermuxPackage+" ") {
			continue
		}
		for _, tok := range strings.Fields(line) {
			raw, ok := strings.CutPrefix(tok, "uid:")
			if !ok {
				continue
			}
			if uid, err := strconv.Atoi(raw); err == nil {
				return uid
			}
		}
	}
	return -1
}

// Available reports whether the Termux environment looks usable: the app is
// installed, the user is resolvable and the package manager binary exists.
func Available() bool {
	if ResolveUID() < 0 {
		return false
	}
	if _, err := os.Stat(TermuxBin + "/pkg"); err != nil {
		return false
	}
	return true
}

// AvailableReason returns a human-readable explanation when Available is
// false, or the empty string when the environment is usable.
func AvailableReason() string {
	if ResolveUID() < 0 {
		return "Termux application is not installed on this device"
	}
	if _, err := os.Stat(TermuxBin + "/pkg"); err != nil {
		return "Termux package manager ($PREFIX/bin/pkg) not found"
	}
	return ""
}

// TermuxCommand builds an *exec.Cmd that runs script as the Termux user with
// the Termux environment applied. The identity change goes through `su <uid>`
// with the Termux supplementary groups (see aidInet); the server's own root
// status is irrelevant because the goal is to *become* the Termux user, not to
// gain root.
//
// It is exported so sibling packages (for example internal/cron) can execute
// work in the same identity the Store tab uses.
func TermuxCommand(ctx context.Context, script string) (*exec.Cmd, error) {
	return termuxCommand(ctx, suBin, script)
}

// SuBin reports the su binary this package uses to enter the Termux user. It is
// exposed for callers that must build their own `su <uid> -c` invocation.
func SuBin() string {
	return suBin
}

// BuildEnv returns the explicit Termux environment. Exporting it lets callers
// that spawn detached processes reproduce the same environment.
func BuildEnv() []string {
	return buildEnv()
}

// quoteSingle wraps a literal in single quotes for safe interpolation into a
// shell command. Single quotes cannot appear inside a single-quoted POSIX
// string, so any embedded quote is closed, escaped and reopened ('"'"'), which
// keeps an arbitrary path or payload inert.
func quoteSingle(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// TermuxRuntimeDir is a small writable directory the server owns for runtime
// bookkeeping (PID files, locks). It lives under the Termux app files tree so
// it stays writable by commands that run as the Termux user and its files can
// be read back by the panel without crossing SELinux contexts.
func TermuxRuntimeDir() string {
	return TermuxHome + "/.tmon"
}

// TermuxRuntime returns the absolute path of a runtime file, suitable for
// interpolation into a shell command. name must be a bare file name.
func TermuxRuntime(name string) string {
	return TermuxRuntimeDir() + "/" + name
}

// WriteStateFile stores a small runtime value under the Termux runtime
// directory. Failures are returned so callers can decide whether they matter.
//
// The server runs as root while every engine it launches runs as the Termux
// user. A directory or file created by root therefore carries root ownership
// and mode 0700/0600, which the Termux user cannot write: the launched shell
// fails to redirect its log and to drop its PID file, silently breaking the
// start path. To keep the runtime tree usable by both identities the
// directory is chowned to the Termux user and made group-writable, and the
// file itself is chowned to the Termux user as well.
func WriteStateFile(name, value string) error {
	dir := TermuxRuntimeDir()
	if err := os.MkdirAll(dir, 0o770); err != nil {
		return err
	}
	_ = ChownTermux(dir)
	_ = os.Chmod(dir, 0o770)
	path := TermuxRuntime(name)
	if err := os.WriteFile(path, []byte(value), 0o660); err != nil {
		return err
	}
	_ = ChownTermux(path)
	_ = os.Chmod(path, 0o660)
	return nil
}

// PrepareRuntimeDir ensures the Termux runtime directory exists, is owned by
// the Termux user and is writable by both the server (root) and that user. It
// is idempotent and safe to call before any command that redirects into the
// directory, which is the failure mode that otherwise turns a start into a
// silent no-op.
func PrepareRuntimeDir() error {
	dir := TermuxRuntimeDir()
	if err := os.MkdirAll(dir, 0o770); err != nil {
		return err
	}
	if err := ChownTermux(dir); err != nil {
		return err
	}
	return os.Chmod(dir, 0o770)
}

// ChownTermux changes a path's owner (and group, when resolvable) to the
// Termux application user so commands running as that identity can write to
// it. When Termux is not installed it is a no-op success.
func ChownTermux(path string) error {
	uid := ResolveUID()
	if uid < 0 {
		return nil
	}
	// An Android app's primary GID equals its UID on the default (single)
	// Android user; multi-user offsets are not worth the complexity here
	// because the mode bits already grant the owner access.
	return os.Chown(path, uid, uid)
}

// AID_INET is Android's "inet" group (GID 3003). The Android kernel enforces
// "paranoid networking": a process running as a *non-root* UID may only create
// network sockets when it carries this supplementary group. MagiskSU's
// `su <uid>` drops every supplementary group, so a command hopping into the
// Termux user loses AID_INET and every socket() call fails with EACCES
// ("socket: Permission denied") even though the same command succeeds as root.
// Passing `-G 3003` restores the group and therefore network access.
const aidInet = 3003

// aidEverybody (9997) is the shared "everybody" group every Android app
// carries; the per-user cache and shared groups (20000+appId and 50000+appId)
// are derived from the Termux UID so a device with a different user index still
// matches the real Termux process.
const (
	aidEverybody = 9997
	aidAppStart  = 10000
	aidUserBase  = 20000
	aidAllBase   = 50000
)

// termuxGroups returns the supplementary GIDs the real Termux process runs
// with, so a `su` invocation can reproduce them. Order mirrors what Android
// assigns to an app: inet, everybody, per-user cache, per-user shared.
//
// Android derives these from the app id (AID_APP_START..): for uid 10171 the
// per-user cache group is 20171 and the shared group 50171, i.e. the 20000/50000
// bases plus (uid%100000 - AID_APP_START). The user index is folded back in via
// uid/100000 so a device on a secondary user still matches.
func termuxGroups(uid int) []int {
	appID := uid%100000 - aidAppStart
	if appID < 0 {
		appID = uid % 100000
	}
	userOffset := (uid / 100000) * 100000
	return []int{
		aidInet,
		aidEverybody,
		userOffset + aidUserBase + appID,
		userOffset + aidAllBase + appID,
	}
}

// TermuxGroups is the exported form of termuxGroups. It is used by the web
// terminal, which reproduces the same identity with syscall.Credential
// instead of `su`; without these groups the shell loses AID_INET and every
// socket() call fails with EACCES (see aidInet).
func TermuxGroups(uid int) []int {
	return termuxGroups(uid)
}

// termuxScript builds the shell body that applies the Termux environment and
// then runs script.
func termuxScript(script string) string {
	return "export HOME=" + TermuxHome +
		"; export PREFIX=" + TermuxPrefix +
		"; export TMPDIR=" + TermuxTmp +
		"; export PATH=" + TermuxBin + ":" + TermuxBin + "/applets" +
		"; export LANG=C.UTF-8" +
		"; export TERM=xterm-256color" +
		"; cd " + TermuxHome +
		"; " + script
}

// suGroupsMu guards suGroupsState, which caches whether the su binary accepts
// the `-G <gid>` supplementary-group flag. -1 means "not probed yet".
var (
	suGroupsMu    sync.Mutex
	suGroupsState = -1
)

// suSupportsGroups reports whether su accepts `-G <gid>`. The result is probed
// once and cached for the process lifetime. The probe runs `su -G 0 -c true`
// and treats a successful exit as support. When su itself is missing or cannot
// be executed there is nothing to gain from `-G`, so the plain form is used;
// any other failure (a policy denial of the dummy command) is assumed to be a
// transient environment issue and `-G` is kept, because every su on Android 8+
// (MagiskSU, KernelSU, APatch, AOSP su) does parse the flag.
func suSupportsGroups(subin string) bool {
	suGroupsMu.Lock()
	defer suGroupsMu.Unlock()
	if suGroupsState != -1 {
		return suGroupsState == 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), ProbeTimeout)
	defer cancel()
	err := exec.CommandContext(ctx, subin, "-G", "0", "-c", "true").Run()
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		suGroupsState = 0
		return false
	}
	suGroupsState = 1
	return true
}

// termuxCommand builds an *exec.Cmd that runs script as the Termux user with
// the Termux environment applied. subin is the su binary used to enter the
// Termux identity; see suBin for why it is not config.SUBin.
//
// The su call passes the Termux supplementary groups (`-G`) so the non-root
// process retains AID_INET and can open sockets; without it every network
// operation fails (see aidInet). On an su that cannot parse `-G` the call
// degrades to the plain `su <uid> -c` form, preserving the previous behaviour.
func termuxCommand(ctx context.Context, subin, script string) (*exec.Cmd, error) {
	uid := ResolveUID()
	if uid < 0 {
		return nil, fmt.Errorf("termux user not found")
	}
	full := termuxScript(script)
	args := suArgs(subin, uid, full)
	cmd := exec.CommandContext(ctx, subin, args...)
	cmd.Env = buildEnv()
	return cmd, nil
}

// suArgs returns the argument vector for a su invocation, including the `-G`
// group flags only when the su binary supports them.
func suArgs(subin string, uid int, full string) []string {
	if !suSupportsGroups(subin) {
		return []string{strconv.Itoa(uid), "-c", full}
	}
	args := []string{strconv.Itoa(uid)}
	for _, gid := range termuxGroups(uid) {
		args = append(args, "-G", strconv.Itoa(gid))
	}
	return append(args, "-c", full)
}

// TermuxUserCommand is TermuxCommand without the `su <uid>` hop: it runs the
// process directly under the server's own identity (root) while still applying
// the Termux environment. Root is the one identity on the phone whose network
// namespace is unrestricted, so a port bind that the Magisk SELinux domain
// denies to the Termux user succeeds here; the engine only reads the site
// directory and never needs write access to it.
func TermuxUserCommand(ctx context.Context, script string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", termuxScript(script))
	cmd.Env = buildEnv()
	return cmd, nil
}
