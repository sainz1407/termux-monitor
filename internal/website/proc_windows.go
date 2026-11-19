//go:build windows

package website

import (
	"os"
	"syscall"
)

// processAlive is the Windows fallback. The panel only ships for Android, but
// the package must still compile and its tests must run on a developer's
// desktop, so the same contract is honoured with the portable process API.
func processAlive(pid int) error {
	if pid <= 0 {
		return syscall.ESRCH
	}
	info, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := info.Signal(syscall.Signal(0)); err != nil {
		return err
	}
	return nil
}

// signalProcess is the Windows fallback: process groups and POSIX signals do
// not exist there, so it degrades to a plain signal to the process.
func signalProcess(sig syscall.Signal, pid int) error {
	if pid <= 0 {
		return syscall.ESRCH
	}
	info, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return info.Signal(sig)
}
