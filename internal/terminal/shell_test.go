package terminal

import (
	"strings"
	"testing"
)

func TestAndroidUserName(t *testing.T) {
	cases := map[uint32]string{
		0:     "u0",
		1000:  "u1000",
		10171: "u0_a171",
	}
	for uid, want := range cases {
		if got := androidUserName(uid); got != want {
			t.Fatalf("androidUserName(%d) = %q, want %q", uid, got, want)
		}
	}
}

func TestShellCandidatesAlwaysProvideFallback(t *testing.T) {
	candidates := shellCandidates()
	if len(candidates) == 0 {
		t.Fatal("shellCandidates returned no candidates")
	}
	last := candidates[len(candidates)-1]
	if len(last.Argv) == 0 || last.Argv[0] != "/system/bin/sh" {
		t.Fatalf("last candidate = %v, want /system/bin/sh", last.Argv)
	}
	for i, cand := range candidates {
		if cand.Label == "" {
			t.Fatalf("candidate %d has no label", i)
		}
		if len(cand.Env) == 0 {
			t.Fatalf("candidate %d has no environment", i)
		}
		if cand.Dir == "" {
			t.Fatalf("candidate %d has no working directory", i)
		}
	}
}

func TestShellRecipeKeepsRootUser(t *testing.T) {
	spec := ShellSpec{
		Argv:  []string{"/system/bin/sh"},
		Env:   []string{"HOME=/data/adb"},
		Dir:   "/data/adb",
		Label: "sh",
	}
	cmd := shellRecipe(spec)
	if cmd.SysProcAttr != nil {
		t.Fatal("root shell must not carry a credential")
	}
	if cmd.Dir != "/data/adb" {
		t.Fatalf("dir = %q, want /data/adb", cmd.Dir)
	}
}

// A Termux shell must expose the Termux prefix variables so `pkg` and the
// other Termux tools resolve, and it must run as the Termux user.
func TestTermuxEnvCarriesPrefixVariables(t *testing.T) {
	profile := &termuxProfile{
		uid:    10171,
		prefix: "/data/data/com.termux/files/usr",
		home:   "/data/data/com.termux/files/home",
		tmp:    "/data/local/tmp",
	}
	env := strings.Join(termuxEnv(profile), "\n")
	for _, want := range []string{
		"HOME=/data/data/com.termux/files/home",
		"PREFIX=/data/data/com.termux/files/usr",
		"PATH=/data/data/com.termux/files/usr/bin:/system/bin:/sbin",
		"LD_LIBRARY_PATH=/data/data/com.termux/files/usr/lib",
		"USER=u0_a171",
		"LOGNAME=u0_a171",
	} {
		if !strings.Contains(env, want) {
			t.Fatalf("environment misses %q", want)
		}
	}
}

func TestShellRecipeRunsAsTermuxUser(t *testing.T) {
	spec := ShellSpec{
		Argv:  []string{"/data/data/com.termux/files/usr/bin/bash", "-i"},
		UID:   10171,
		Env:   []string{"HOME=/data/data/com.termux/files/home"},
		Dir:   "/data/data/com.termux/files/home",
		Label: "termux bash",
	}
	cmd := shellRecipe(spec)
	if cmd.Args[0] != spec.Argv[0] {
		t.Fatalf("argv = %v, want %v", cmd.Args, spec.Argv)
	}
}
