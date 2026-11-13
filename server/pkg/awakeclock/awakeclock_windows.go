//go:build windows

package awakeclock

import (
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows' runtime monotonic clock (interrupt time) keeps counting while the
// host sleeps or hibernates. Both readings below count from boot:
//
//   - GetTickCount64 includes sleep (ms resolution).
//   - QueryUnbiasedInterruptTime excludes it (100 ns units).
//
// Their difference is the total time the host has spent asleep since boot.
// Both are available since Windows 7.
// https://learn.microsoft.com/en-us/windows/win32/sysinfo/interrupt-time
var (
	kernel32                       = windows.NewLazySystemDLL("kernel32.dll")
	procGetTickCount64             = kernel32.NewProc("GetTickCount64")
	procQueryUnbiasedInterruptTime = kernel32.NewProc("QueryUnbiasedInterruptTime")

	originSlept = hostSleptSinceBoot()
	// maxSlept keeps the correction monotone. The tick count has
	// millisecond resolution, so successive differences can jitter down by a
	// few ms. Total slept time never actually decreases.
	maxSlept atomic.Int64
)

func sleptSinceOrigin() time.Duration {
	slept := sleptDelta(originSlept, hostSleptSinceBoot())
	for {
		previous := maxSlept.Load()
		if int64(slept) <= previous {
			return time.Duration(previous)
		}
		if maxSlept.CompareAndSwap(previous, int64(slept)) {
			return slept
		}
	}
}

// hostSleptSinceBoot returns total host sleep since boot, or 0 if the APIs
// cannot be called. In that case the clock falls back to the runtime
// monotonic clock, which matches the behaviour before this package existed.
func hostSleptSinceBoot() time.Duration {
	if procGetTickCount64.Find() != nil || procQueryUnbiasedInterruptTime.Find() != nil {
		return 0
	}
	tickMs, _, _ := procGetTickCount64.Call()
	var unbiased100ns uint64
	ok, _, _ := procQueryUnbiasedInterruptTime.Call(uintptr(unsafe.Pointer(&unbiased100ns)))
	if ok == 0 {
		return 0
	}
	return sleptFromReadings(uint64(tickMs), unbiased100ns)
}
