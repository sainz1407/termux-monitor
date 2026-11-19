//go:build linux || android

package terminal

import (
	"os"
	"syscall"
)

// fileOwnerUID returns the uid owning a file on unix-like systems.
func fileOwnerUID(info os.FileInfo) (uint32, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return stat.Uid, true
}

// credentialAttr builds the process attributes that run the shell as another
// user. The pty master is allocated before the privilege drop, so the child
// keeps its controlling terminal and job control still works.
//
// groups must list the target user's supplementary groups. Android's "paranoid
// networking" only lets a non-root process call socket() when it carries
// AID_INET (3003), so a credential that switches Uid without Groups leaves the
// shell without network access: DNS still works (it goes through a unix
// socket) but every TCP connection fails with EACCES, which is what made the
// mirror probe report all repos as "bad". Passing nil keeps the parent's
// groups, which is only correct for a non-root daemon.
func credentialAttr(uid uint32, groups []int) *syscall.SysProcAttr {
	// syscall.Credential.Groups is []uint32 on Linux/Android; the rest of the
	// package keeps GIDs as int for store.TermuxGroups parity.
	var gids []uint32
	if len(groups) > 0 {
		gids = make([]uint32, len(groups))
		for i, gid := range groups {
			gids[i] = uint32(gid)
		}
	}
	return &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uid, Gid: uid, Groups: gids},
	}
}
