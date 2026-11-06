//go:build !windows

package daemon

import (
	"errors"
	"syscall"
)

// processAlive probes pid without sending a signal. ESRCH is the only result
// that proves the process is gone; permission and other errors fail safe to
// alive so GC cannot act on an inconclusive ownership check.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return !errors.Is(err, syscall.ESRCH)
}
