//go:build windows

package util

import (
	"syscall"
	"unsafe"
)

var (
	kernel32                    = syscall.NewLazyDLL("kernel32.dll")
	user32                      = syscall.NewLazyDLL("user32.dll")
	procAllocConsole            = kernel32.NewProc("AllocConsole")
	procGetConsoleWnd           = kernel32.NewProc("GetConsoleWindow")
	procGetConsoleProcessList   = kernel32.NewProc("GetConsoleProcessList")
	procShowWindow              = user32.NewProc("ShowWindow")
)

const swHide = 0

// EnsureHiddenConsole guarantees the daemon owns a console that is not
// visible to the user. Every child process (git, cmd, etc.) inherits this
// hidden console automatically, so no per-call SysProcAttr gymnastics are
// needed.
//
// MUST be called at daemon startup before any child process is spawned —
// the inherited hidden console only protects children started after this
// returns. A child spawned earlier will allocate its own visible console
// window and reintroduce the popup-flash regression from #2357.
func EnsureHiddenConsole() {
	if hwnd, _, _ := procGetConsoleWnd.Call(); hwnd != 0 {
		// A console already exists. When another process is attached — the
		// cmd/PowerShell that the user typed into, Windows Terminal's shell
		// — it is the hosting terminal and hiding it would hide the user's
		// own window. When we are the ONLY process on it, Windows created
		// it for this launch (registry Run key, Task Scheduler,
		// double-click), and leaving it visible would park a console window
		// on the desktop for the daemon's entire lifetime — exactly the
		// login-time popup the boot-autostart registration would otherwise
		// introduce.
		if consoleProcessCount() == 1 {
			procShowWindow.Call(hwnd, swHide)
		}
		return
	}
	if r, _, _ := procAllocConsole.Call(); r == 0 {
		return // AllocConsole failed
	}
	if hwnd, _, _ := procGetConsoleWnd.Call(); hwnd != 0 {
		procShowWindow.Call(hwnd, swHide)
	}
}

// consoleProcessCount reports how many processes are attached to this
// process's console, or 0 when there is no console or the query fails.
// Anything above 1 means a hosting shell is present; a failed query reading
// 0 leaves the window alone, which is the conservative outcome.
func consoleProcessCount() uint32 {
	var buf [2]uint32
	n, _, _ := procGetConsoleProcessList.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	return uint32(n)
}
