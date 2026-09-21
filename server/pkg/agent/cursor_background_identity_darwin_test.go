//go:build darwin

package agent

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
	"unsafe"
)

func TestCaptureCursorBackgroundKernelRejectsStaleIdentity(t *testing.T) {
	var layout cursorDarwinIdentity
	if unsafe.Sizeof(layout) != 56 || unsafe.Offsetof(layout.unique) != 16 || unsafe.Offsetof(layout.version) != 32 {
		t.Fatal("Darwin process identity ABI layout changed")
	}
	child := exec.Command(os.Args[0])
	child.Env = append(os.Environ(), cursorFakeModeEnv+"=leaf", "CURSOR_FAKE_DURATION=30s")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	identity, err := readCursorDarwinIdentity(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := signalCursorDarwinIdentity(child.Process.Pid, identity.version+1, syscall.SIGKILL); err != syscall.ESRCH {
		t.Fatalf("kernel accepted a stale PID version: %v", err)
	}
	if err := child.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("stale identity signalled a live process: %v", err)
	}
	if err := signalCursorDarwinIdentity(child.Process.Pid, identity.version, syscall.SIGKILL); err != nil {
		t.Fatalf("kernel could not signal the verified identity: %v", err)
	}
	assertCursorTestProcessGone(t, child.Process.Pid)
}
