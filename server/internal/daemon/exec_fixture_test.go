package daemon

import (
	"os"
	"syscall"
	"testing"
)

// writeTestExecutable writes a fake CLI with exec perms while holding
// syscall.ForkLock.RLock, so no concurrent t.Parallel() sibling can fork
// between our OpenFile and Close. Without it, the sibling's fork child
// inherits the still-open write fd and the exec of this file fails with Linux
// ETXTBSY ("text file busy") (Go #22315). Windows has no such race; holding
// the lock there is harmless.
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
