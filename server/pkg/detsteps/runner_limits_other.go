//go:build !unix

package detsteps

import "time"

// limitChildCPU is a no-op on platforms without POSIX rlimits (e.g. Windows).
// The parent's wall-clock SIGKILL remains the preemption mechanism there.
func limitChildCPU(time.Duration) {}
