//go:build !windows

package platform

import (
	"os"
	"syscall"
)

// ShutdownSignals returns the OS signals that should trigger a graceful shutdown.
func ShutdownSignals() []os.Signal {
	return []os.Signal{syscall.SIGINT, syscall.SIGTERM}
}
