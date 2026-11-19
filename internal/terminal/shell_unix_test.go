//go:build linux || android

package terminal

import (
	"slices"
	"testing"

	"termux-monitor/internal/store"
)

// The Termux shell must run as the Termux user, otherwise the Termux package
// manager refuses to work: `pkg` exits with "Cannot run 'pkg' command as root".
func TestTermuxShellCarriesCredential(t *testing.T) {
	spec := ShellSpec{
		Argv:   []string{"/data/data/com.termux/files/usr/bin/bash", "-i"},
		UID:    10171,
		Groups: store.TermuxGroups(10171),
		Env:    []string{"HOME=/data/data/com.termux/files/home"},
		Dir:    "/data/data/com.termux/files/home",
		Label:  "termux bash",
	}
	cmd := shellRecipe(spec)
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.Credential == nil {
		t.Fatal("termux shell must carry a credential")
	}
	cred := cmd.SysProcAttr.Credential
	if cred.Uid != 10171 || cred.Gid != 10171 {
		t.Fatalf("credential = %+v, want uid/gid 10171", cred)
	}
	// Android's paranoid networking blocks socket() for a non-root process
	// without AID_INET, so the group must survive the privilege drop.
	if !slices.Contains(cred.Groups, 3003) {
		t.Fatalf("credential groups = %v, want AID_INET 3003", cred.Groups)
	}
}
