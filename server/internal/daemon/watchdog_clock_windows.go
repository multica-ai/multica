package daemon

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Resolve through the Windows 10 API set: kernel32.dll does not export the
// precise variant, even though the API documentation lists it as the DLL.
var queryUnbiasedInterruptTimePrecise = windows.NewLazySystemDLL("api-ms-win-core-realtime-l1-1-1.dll").NewProc("QueryUnbiasedInterruptTimePrecise")

// Go's Windows monotonic clock includes suspend. This Windows 10+ API instead
// returns awake time in 100ns units, unaffected by sleep or wall-clock changes.
// It returns VOID; GetLastError is not a success/failure result for this call.
// https://learn.microsoft.com/windows/win32/api/realtimeapiset/nf-realtimeapiset-queryunbiasedinterrupttimeprecise
func newWatchdogClock() func() time.Duration {
	return func() time.Duration {
		var ticks uint64
		queryUnbiasedInterruptTimePrecise.Call(uintptr(unsafe.Pointer(&ticks)))
		return time.Duration(ticks) * 100 * time.Nanosecond
	}
}
