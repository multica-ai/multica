//go:build windows

package platform

import "os"

// ShutdownSignals returns the OS signals that should trigger a graceful shutdown.
// On Windows, only os.Interrupt (Ctrl+C) is reliably supported.
func ShutdownSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
