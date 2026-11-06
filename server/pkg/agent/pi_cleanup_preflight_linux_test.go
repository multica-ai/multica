//go:build linux

package agent

import (
	"errors"
	"os"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestProbePiPidfd(t *testing.T) {
	// Stubs keep the test independent of the host kernel and seccomp profile,
	// and fail if the probe ever targets another process or sends a real signal.
	openedFD := -1
	openOK := func(pid, flags int) (int, error) {
		if pid != os.Getpid() || flags != 0 {
			t.Fatalf("pidfd_open(%d, %d), want own pid and flags 0", pid, flags)
		}
		// A real fd lets the probe's deferred close run normally.
		var p [2]int
		if err := unix.Pipe(p[:]); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { unix.Close(p[1]) })
		openedFD = p[0]
		return p[0], nil
	}
	sendOK := func(fd int, sig unix.Signal, info *unix.Siginfo, flags int) error {
		if sig != 0 || info != nil || flags != 0 {
			t.Fatalf("pidfd_send_signal(sig=%d, info=%v, flags=%d), want probe-only call", sig, info, flags)
		}
		return nil
	}
	tests := []struct {
		name string
		open func(int, int) (int, error)
		send func(int, unix.Signal, *unix.Siginfo, int) error
		want error
	}{
		{"supported", openOK, sendOK, nil},
		// Linux < 5.3, as reported on 4.9.
		{"pidfd_open missing", func(int, int) (int, error) { return -1, syscall.ENOSYS }, sendOK, syscall.ENOSYS},
		{"pidfd_open denied", func(int, int) (int, error) { return -1, syscall.EPERM }, sendOK, syscall.EPERM},
		{"pidfd_send_signal missing", openOK, func(int, unix.Signal, *unix.Siginfo, int) error { return syscall.ENOSYS }, syscall.ENOSYS},
		{"pidfd_send_signal denied", openOK, func(int, unix.Signal, *unix.Siginfo, int) error { return syscall.EPERM }, syscall.EPERM},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			openedFD = -1
			err := probePiPidfd(tt.open, tt.send)
			if openedFD >= 0 {
				if _, ferr := unix.FcntlInt(uintptr(openedFD), unix.F_GETFD, 0); !errors.Is(ferr, syscall.EBADF) {
					t.Fatalf("pidfd %d left open after probe", openedFD)
				}
			}
			if tt.want == nil {
				if err != nil {
					t.Fatalf("supported environment rejected: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("error=%v, want %v", err, tt.want)
			}
		})
	}
}
