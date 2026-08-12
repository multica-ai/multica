//go:build windows

package daemon

import (
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

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
			return windows.UTF16ToString(buffer[:length]), nil
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
