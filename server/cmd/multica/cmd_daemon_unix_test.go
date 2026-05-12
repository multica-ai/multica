//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestSetupGoroutineDumpWritesFileOnSIGQUIT(t *testing.T) {
	dir := t.TempDir()
	setupGoroutineDump(dir)

	if err := syscall.Kill(os.Getpid(), syscall.SIGQUIT); err != nil {
		t.Fatalf("send SIGQUIT: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var matches []string
	for time.Now().Before(deadline) {
		var err error
		matches, err = filepath.Glob(filepath.Join(dir, "multica-goroutines-*.txt"))
		if err != nil {
			t.Fatalf("glob dump files: %v", err)
		}
		if len(matches) > 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if len(matches) == 0 {
		t.Fatal("expected SIGQUIT to write a goroutine dump file")
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read dump file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("goroutine dump file was empty")
	}
}
