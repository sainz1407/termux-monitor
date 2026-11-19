//go:build !unix

package procmon

import "fmt"

// Kill is not available on platforms without Unix process signals.
func (m *Monitor) Kill(pid int, force bool) error {
	return fmt.Errorf("terminating processes is not supported on this platform")
}
