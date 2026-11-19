//go:build !windows

package website

import "syscall"

// processAlive reports whether pid names a live process by issuing the kill(2)
// null signal from the server process.
//
// It deliberately does not shell out to `kill -0`: Android ships toybox as
// /system/bin/kill, and toybox parses `-0` as a flag rather than as signal
// number zero (`kill: Unknown signal '(null)'`), so every such probe fails and
// a running engine is reported dead. A direct syscall has no such ambiguity and
// avoids spawning a process per poll.
//
// Probing from root is also deliberate: a process started by the server cannot
// be signaled from a `su <uid>` child (different SELinux domain), so going back
// through su would report every running engine as dead.
func processAlive(pid int) error {
	if pid <= 0 {
		return syscall.ESRCH
	}
	// Signal 0 performs error checking only: it succeeds when the process
	// exists and is signalable, and returns ESRCH when it does not.
	return syscall.Kill(pid, 0)
}

// signalProcess sends sig to pid's whole process group, falling back to the
// bare PID when the process is not a group leader. The group form cleans up any
// children the engine spawned; it talks to the kernel directly so it does not
// depend on an external kill binary (see processAlive).
func signalProcess(sig syscall.Signal, pid int) error {
	if pid <= 0 {
		return syscall.ESRCH
	}
	if err := syscall.Kill(-pid, sig); err == nil {
		return nil
	}
	return syscall.Kill(pid, sig)
}
