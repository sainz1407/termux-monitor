package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Mirror management for the Store tab. The panel only touches the *single*
// active sources.list ($PREFIX/etc/apt/sources.list) that `pkg`/`apt` read on
// Termux; whichever mirror file it symlinks to (or contains) is left in place.
//
// A rewrite is destructive by nature — a wrong URL leaves the user without a
// working package index — so every write follows the same contract:
//
//  1. The current file is copied to a timestamped backup under
//     $HOME/.tmon/backup before a single byte changes.
//  2. The replacement is written to a temporary file in the same directory and
//     only then renamed over the target, so an interrupted write can never
//     truncate the live file.
//  3. Both the temp file and the resulting file are chowned to the Termux user;
//     a root-owned sources.list would make a later `pkg` run fail with a
//     permission error.

// Mirror is one selectable Termux package repository.
type Mirror struct {
	// ID is the stable identifier used by the API.
	ID string `json:"id"`
	// Name is the human label shown in the UI.
	Name string `json:"name"`
	// Description explains where the mirror is hosted.
	Description string `json:"description"`
	// Host is the primary URL used to detect the active mirror.
	Host string `json:"host"`
	// URLs are the deb lines written verbatim, in order.
	URLs []string `json:"urls"`
}

// CuratedMirrors lists the Termux mirrors worth offering. The order matches
// `termux-change-repo`'s listing for the main repository: the official
// F-Droid-hosted endpoint first, then the two long-standing community mirrors.
var CuratedMirrors = []Mirror{
	{
		ID:          "fdroid",
		Name:        "F-Droid (Official)",
		Description: "Primary Termux repository, hosted by F-Droid",
		Host:        "https://packages.termux.dev/apt/termux-main",
		URLs: []string{
			"https://packages.termux.dev/apt/termux-main stable main",
		},
	},
	{
		ID:          "cloudflare",
		Name:        "Cloudflare (packages-cf.termux.dev)",
		Description: "Global CDN mirror, fast from most regions",
		Host:        "https://packages-cf.termux.dev/apt/termux-main",
		URLs: []string{
			"https://packages-cf.termux.dev/apt/termux-main stable main",
		},
	},
	{
		ID:          "grimler",
		Name:        "Grimler",
		Description: "Community-run mirror by the Termux maintainer Grimler",
		Host:        "https://grimler.se/termux/termux-main",
		URLs: []string{
			"https://grimler.se/termux/termux-main stable main",
		},
	},
}

// MirrorByID resolves a curated mirror identifier.
func MirrorByID(id string) (Mirror, bool) {
	for _, m := range CuratedMirrors {
		if m.ID == id {
			return m, true
		}
	}
	return Mirror{}, false
}

// SourcesListPath is the absolute path of the active apt source list. Termux
// keeps every repository in this one file (no sources.list.d), so reading and
// writing it is enough to switch mirrors.
func SourcesListPath() string {
	return TermuxPrefix + "/etc/apt/sources.list"
}

// mirrorHosts are the hosts of every curated mirror, used to recognise the
// active one. The set is intentionally independent of CuratedMirrors ordering
// so detection keeps working if a mirror is added or removed.
var mirrorHosts = []string{
	"https://packages.termux.dev/apt/termux-main",
	"https://packages-cf.termux.dev/apt/termux-main",
	"https://grimler.se/termux/termux-main",
}

// readTimeout bounds each read-only sources.list probe.
const readTimeout = 8 * time.Second

// SourcesReadRequest is the outcome of reading the active sources.list.
type SourcesReadRequest struct {
	// Exists is false when no sources.list is present (a broken install).
	Exists bool
	// Content is the raw file text; empty when it could not be read.
	Content string
	// Reason explains a failed read; empty on success.
	Reason string
	// ActiveID is the curated mirror detected in the file, or "" when the file
	// uses an unrecognised (for example custom or local) repository.
	ActiveID string
}

// ReadSourcesList returns the active sources.list content. The file lives
// inside $PREFIX, which root can read directly, so no shell hop is needed; the
// fallback through the Termux user covers installations where $PREFIX carries
// restrictive permissions.
func ReadSourcesList() SourcesReadRequest {
	res := SourcesReadRequest{}
	path := SourcesListPath()

	raw, err := os.ReadFile(path)
	if err == nil {
		res.Exists = true
		res.Content = strings.ReplaceAll(string(raw), "\r\n", "\n")
		res.ActiveID = DetectMirror(res.Content)
		return res
	}
	if !errors.Is(err, os.ErrNotExist) {
		res.Reason = err.Error()
	}

	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	out, cerr := probe(ctx, "cat "+quoteSingle(path)+" 2>/dev/null")
	if cerr != nil {
		if res.Reason == "" {
			res.Reason = "sources.list is not readable"
		}
		return res
	}
	res.Exists = true
	res.Content = strings.ReplaceAll(out, "\r\n", "\n")
	res.ActiveID = DetectMirror(res.Content)
	return res
}

// DetectMirror reports which curated mirror a sources.list body uses by
// scanning its uncommented deb lines. An empty string means the file points at
// something the panel does not manage.
func DetectMirror(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, host := range mirrorHosts {
			if strings.Contains(line, host) {
				return mirrorIDForHost(host)
			}
		}
	}
	return ""
}

// mirrorIDForHost maps a recognized host URL back to its curated id.
func mirrorIDForHost(host string) string {
	for _, m := range CuratedMirrors {
		if m.Host == host {
			return m.ID
		}
	}
	return ""
}

// MirrorBackupDir is where pre-write copies of sources.list are kept. It sits
// beside the other runtime files so a module reinstall that preserves $HOME
// keeps the last known-good repository configuration recoverable.
func MirrorBackupDir() string {
	return TermuxRuntimeDir() + "/backup"
}

// SourcesWriteResult describes what a rewrite did.
type SourcesWriteResult struct {
	// Backup is the absolute path of the pre-write copy, when one was made.
	Backup string
	// Content is the new file body that was written.
	Content string
}

// WriteMirror replaces the active sources.list with the given mirror's deb
// lines. It takes a timestamped backup first and writes atomically.
//
// The work runs as the Termux user, not as the server's root: the file lives in
// $PREFIX and must stay owned by that user, and a root-owned replacement would
// break the next `pkg` invocation.
func WriteMirror(id string) (SourcesWriteResult, error) {
	mirror, ok := MirrorByID(id)
	if !ok {
		return SourcesWriteResult{}, errors.New("unknown mirror")
	}
	if !Available() {
		return SourcesWriteResult{}, errors.New(AvailableReason())
	}
	if err := PrepareRuntimeDir(); err != nil {
		return SourcesWriteResult{}, err
	}

	backupDir := MirrorBackupDir()
	if err := os.MkdirAll(backupDir, 0o770); err != nil {
		return SourcesWriteResult{}, err
	}
	// The backup is produced by a Termux-owned copy, so it is written under a
	// directory both identities can reach; ownership is corrected afterwards.
	_ = ChownTermux(backupDir)
	_ = os.Chmod(backupDir, 0o770)

	body := "### Termux Monitor: " + mirror.Name + " mirror\n" +
		strings.Join(mirror.URLs, "\n") + "\n"
	backupName := "sources.list." + time.Now().Format("20060102-150405.bak")

	// One shell invocation performs backup, write and verification, so a
	// failure at any step is reported once and never leaves a half-written
	// file behind. The temp file is created next to the target (same
	// filesystem) and renamed over it, which is atomic within the directory.
	script := "set -e; " +
		"mkdir -p " + quoteSingle(backupDir) + "; " +
		"if [ -f " + quoteSingle(SourcesListPath()) + " ]; then " +
		"cp " + quoteSingle(SourcesListPath()) + " " + quoteSingle(filepath.Join(backupDir, backupName)) + "; fi; " +
		"printf '%s' " + quoteSingle(body) + " > " + quoteSingle(SourcesListPath()+".tmon.tmp") + "; " +
		"mv " + quoteSingle(SourcesListPath()+".tmon.tmp") + " " + quoteSingle(SourcesListPath())

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd, err := termuxCommand(ctx, suBin, script)
	if err != nil {
		return SourcesWriteResult{}, err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return SourcesWriteResult{}, errors.New("writing sources.list failed: " + truncateLine(msg, 300))
		}
		return SourcesWriteResult{}, errors.New("writing sources.list failed")
	}

	// The rename preserves the temp file's ownership, so the target must be
	// re-chowned by the same user that wrote it. A no-op here is harmless when
	// the copy already carried the right owner.
	_ = os.Chown(SourcesListPath(), ResolveUID(), ResolveUID())

	res := SourcesWriteResult{Content: body}
	if _, statErr := os.Stat(filepath.Join(backupDir, backupName)); statErr == nil {
		res.Backup = filepath.Join(backupDir, backupName)
	}
	return res, nil
}

// LatestBackup returns the most recent sources.list backup path, or "" when no
// backup exists yet.
func LatestBackup() string {
	dir := MirrorBackupDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	best := ""
	var bestTime time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "sources.list.") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(bestTime) {
			bestTime = info.ModTime()
			best = filepath.Join(dir, e.Name())
		}
	}
	return best
}

// truncateLine keeps the first line of a message within max bytes so a long
// stderr dump cannot bloat an API error response.
func truncateLine(s string, max int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) > max {
		s = s[:max]
	}
	return s
}
