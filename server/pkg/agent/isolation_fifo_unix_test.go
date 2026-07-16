//go:build unix

package agent

import (
	"path/filepath"
	"syscall"
	"testing"
)

func assertFIFOReadOnlyFileRejected(t *testing.T, writable string) {
	t.Helper()
	fifo := filepath.Join(writable, "authority.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (TaskIsolationPolicy{
		WritableRoots: []string{writable},
		ReadOnlyFiles: []ReadOnlyFileMount{{
			Source: fifo,
			Target: "/run/multica/task-authority.json",
		}},
	}).Validated(); err == nil {
		t.Fatal("FIFO exact file unexpectedly accepted")
	}
}
