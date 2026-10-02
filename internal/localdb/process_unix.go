//go:build !windows

package localdb

import (
	"errors"
	"syscall"
)

// processAlive reports whether a process exists (signal 0 probes without
// delivering anything).
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
