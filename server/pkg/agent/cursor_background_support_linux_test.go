//go:build linux

package agent

import (
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// requireCursorBackgroundOwnership skips a test that needs the daemon to own a
// Cursor background shell. Ownership signals the shell's process group through
// a pidfd (PIDFD_SIGNAL_PROCESS_GROUP, Linux 6.9+). Older kernels reject the
// flag with EINVAL, and production then logs that it cannot own the shell and
// keeps the idle watchdog instead.
func requireCursorBackgroundOwnership(t *testing.T) {
	t.Helper()
	group, err := captureCursorUnixGroup(os.Getpid())
	switch {
	case err == nil:
		group.close()
	case errors.Is(err, unix.ESRCH):
		// The test binary need not lead a process group. A kernel that knows
		// the flag looks for the group and finds none; an older one rejects the
		// flag before looking.
	case errors.Is(err, unix.EINVAL):
		t.Skip("pidfd process-group signalling needs Linux 6.9 or newer")
	default:
		t.Fatalf("probe pidfd process-group signalling: %v", err)
	}
}
