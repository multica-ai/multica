//go:build windows

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// extendedLengthPrefix and extendedLengthUNCPrefix are the forms
// GetFinalPathNameByHandleW returns with VOLUME_NAME_DOS: a local path comes
// back as `\\?\C:\dir\file`, a network path as `\\?\UNC\server\share\file`.
const (
	extendedLengthPrefix    = `\\?\`
	extendedLengthUNCPrefix = `\\?\UNC\`
)

// trimExtendedLengthPrefix returns path in its plain Win32 form when it is
// short enough not to need the extended-length prefix, and unchanged otherwise.
//
// The prefix is not cosmetic: past MAX_PATH it is the only way to name the
// file, which is why long paths keep it. But it is also not universally
// understood. cmd.exe rejects it outright, and an agent installed through npm
// is entered via a `.cmd` shim that Windows can only run by handing the path to
// the command interpreter — so a launch target that keeps the prefix cannot be
// executed at all. Dropping it whenever it is not load-bearing keeps both
// shapes launchable, and keeps resolved paths comparable with the configured
// paths they are matched against. See #6883.
func trimExtendedLengthPrefix(path string) string {
	var trimmed string
	switch {
	case strings.HasPrefix(path, extendedLengthUNCPrefix):
		trimmed = `\\` + path[len(extendedLengthUNCPrefix):]
	case strings.HasPrefix(path, extendedLengthPrefix):
		trimmed = path[len(extendedLengthPrefix):]
		// Only a drive-qualified path (`C:\...`) is a valid Win32 path without
		// the prefix. Anything else (a volume GUID, say) must keep it.
		if len(trimmed) < 3 || trimmed[1] != ':' || !os.IsPathSeparator(trimmed[2]) {
			return path
		}
	default:
		return path
	}
	if !fitsWithoutExtendedLengthPrefix(trimmed) {
		return path
	}
	return trimmed
}

// fitsWithoutExtendedLengthPrefix reports whether path is short enough to be
// used as a plain Win32 path.
//
// MAX_PATH counts UTF-16 code units and includes the terminating NUL, which is
// why this cannot use len(path): Go measures UTF-8 bytes, and every non-ASCII
// character costs more bytes than it does code units. A path under a user
// directory with a CJK name would be over-counted, judged too long, and keep a
// prefix it does not need — reintroducing the .cmd launch failure for exactly
// the users least likely to be tested against.
func fitsWithoutExtendedLengthPrefix(path string) bool {
	// UTF16FromString returns the string plus its terminating NUL, which is the
	// unit MAX_PATH is expressed in. It fails only on an embedded NUL, which no
	// real path has — treat that as "keep the prefix" rather than guessing.
	encoded, err := windows.UTF16FromString(path)
	if err != nil {
		return false
	}
	return len(encoded) <= syscall.MAX_PATH
}

func canonicalPath(path string) (string, error) {
	pathUTF16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(
		pathUTF16,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)

	buffer := make([]uint16, syscall.MAX_PATH)
	for {
		length, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
		if err != nil {
			return "", err
		}
		if length < uint32(len(buffer)) {
			return trimExtendedLengthPrefix(windows.UTF16ToString(buffer[:length])), nil
		}
		buffer = make([]uint16, length+1)
	}
}

func discoveredExecutablePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func canonicalConfiguredExecutablePath(path string) string {
	return discoveredExecutablePath(path)
}

var executablePathForLaunch = executablePathForLaunchWindows

func executablePathForLaunchWindows(path string) (string, bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", true, err
	}
	resolved, err := canonicalPath(abs)
	return resolved, true, err
}
