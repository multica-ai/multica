package agent

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isPlatformExecFormatError recognises the Windows spelling of "this file is
// not a program I can run".
//
// The stub problem is not POSIX-only: npm 12 blocks install scripts on Windows
// too, so `bin/claude.exe` stays the shipped text placeholder and CreateProcess
// rejects it with ERROR_BAD_EXE_FORMAT — Windows never reports ENOEXEC for it,
// so without this the same broken install would read as a transient hiccup here
// and keep its runtime online.
func isPlatformExecFormatError(err error) bool {
	return errors.Is(err, windows.ERROR_BAD_EXE_FORMAT)
}
