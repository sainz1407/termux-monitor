//go:build unix

package procmon

import (
	"fmt"
	"os"
	"syscall"
)

// Kill terminates a process. A graceful SIGTERM is sent by default, SIGKILL
// is used when force is requested.
func (m *Monitor) Kill(pid int, force bool) error {
	if pid <= 1 {
		return fmt.Errorf("refusing to kill PID %d", pid)
	}
	if pid == os.Getpid() {
		return fmt.Errorf("refusing to kill the monitor process itself")
	}
	if pid == os.Getppid() {
		return fmt.Errorf("refusing to kill the parent process")
	}
	if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err != nil {
		return fmt.Errorf("process %d does not exist", pid)
	}

	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	if err := syscall.Kill(pid, sig); err != nil {
		return fmt.Errorf("failed signalling PID %d: %v", pid, err)
	}
	return nil
}
