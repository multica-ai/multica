package agent

import (
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func observeRuntimeExit(pid int) error {
	// Darwin siginfo_t is 104 bytes on both supported architectures. Only
	// waitid's return value is needed; keep the output aligned and oversized.
	var info [16]uint64
	for {
		_, _, errno := unix.Syscall6(syscall.SYS_WAITID, 1 /* P_PID */, uintptr(pid), uintptr(unsafe.Pointer(&info[0])), unix.WEXITED|unix.WNOWAIT, 0, 0)
		runtime.KeepAlive(&info)
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return errno
		}
		return nil
	}
}
