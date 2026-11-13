//go:build unix

package agent

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
)

// writeTestExecutable writes content to path with exec perms while holding
// syscall.ForkLock.RLock, so no concurrent t.Parallel() sibling can fork
// between our OpenFile and Close. Without this, Linux ETXTBSY fires when
// the sibling's fork child inherits our still-open write fd and the
// subsequent exec of the file sees "text file busy" (seen on CI as
// TestKimiBackendInvokesACPSubcommand: fork/exec ... text file busy).
//
// On macOS the first exec of every new file is held while XProtect scans it:
// ~0.4s on an idle machine and several seconds once parallel tests and other
// builds queue behind the same scanner. The scan is cached per inode and is
// not repeated when the file is rewritten in place, so a new script is first
// written as a no-op, executed once, then truncated to its real content. That
// moves the scan out of the test's own timeouts. Only scripts take this path:
// rewriting an executed Mach-O in place invalidates its code signature.
func writeTestExecutable(tb testing.TB, path string, content []byte) {
	tb.Helper()
	if runtime.GOOS == "darwin" && bytes.HasPrefix(content, []byte("#!")) {
		if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
			writeTestExecutableContent(tb, path, []byte("#!/bin/sh\nexit 0\n"))
			_ = exec.Command(path).Run()
		}
	}
	writeTestExecutableContent(tb, path, content)
}

func writeTestExecutableContent(tb testing.TB, path string, content []byte) {
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
