//go:build unix

package detsteps

import (
	"syscall"
	"time"
)

// limitChildCPU caps the child's CPU time as a kernel-enforced backstop to the
// parent's wall-clock SIGKILL. RLIMIT_CPU is whole seconds; we allow a little
// past the step deadline so the cooperative timeout normally wins and this only
// catches a process the parent somehow failed to kill. Best-effort: a failure
// to set the limit is non-fatal.
func limitChildCPU(timeout time.Duration) {
	secs := uint64(timeout/time.Second) + 2
	if secs < 2 {
		secs = 2
	}
	_ = syscall.Setrlimit(syscall.RLIMIT_CPU, &syscall.Rlimit{Cur: secs, Max: secs})
}
