//go:build unix

package agent

import (
	"os"
	"syscall"
	"testing"
)

// writeTestExecutable writes content to path with exec perms while holding
// syscall.ForkLock.RLock, so no concurrent t.Parallel() sibling can fork
// between our OpenFile and Close. Without this, Linux ETXTBSY fires when
// the sibling's fork child inherits our still-open write fd and the
// subsequent exec of the file sees "text file busy" (seen on CI as
// TestKimiBackendInvokesACPSubcommand: fork/exec ... text file busy).
func writeTestExecutable(tb testing.TB, path string, content []byte) {
	tb.Helper()
	syscall.ForkLock.RLock()
	defer syscall.ForkLock.RUnlock()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		tb.Fatalf("write test executable %s: open: %v", path, err)
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		tb.Fatalf("write test executable %s: write: %v", path, err)
	}
	if err := f.Close(); err != nil {
		tb.Fatalf("write test executable %s: close: %v", path, err)
	}
}
