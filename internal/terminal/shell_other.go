//go:build !linux && !android

package terminal

import (
	"os"
	"syscall"
)

// fileOwnerUID reports that ownership is unavailable on this platform.
func fileOwnerUID(_ os.FileInfo) (uint32, bool) {
	return 0, false
}

// credentialAttr reports that switching users is unavailable on this platform.
func credentialAttr(_ uint32, _ []int) *syscall.SysProcAttr {
	return nil
}
