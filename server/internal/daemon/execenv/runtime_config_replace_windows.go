package execenv

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var replaceFileW = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReplaceFileW")

// replaceRuntimeConfigFile uses ReplaceFileW for an existing target instead
// of Go's MoveFileEx-based os.Rename. ReplaceFileW carries the replaced file's
// ACLs, encryption, compression, named streams, and other attributes onto the
// staged replacement; MoveFileEx would silently replace them with metadata
// inherited by the temp file. A same-directory backup lets us restore the
// original if ReplaceFileW encounters one of its partial-move error states.
func replaceRuntimeConfigFile(stagedPath, targetPath string) error {
	if _, err := os.Lstat(targetPath); errors.Is(err, fs.ErrNotExist) {
		return os.Rename(stagedPath, targetPath)
	} else if err != nil {
		return fmt.Errorf("stat replace target: %w", err)
	}

	backup, err := os.CreateTemp(filepath.Dir(targetPath), "."+filepath.Base(targetPath)+".multica-backup-*.tmp")
	if err != nil {
		return fmt.Errorf("reserve replacement backup: %w", err)
	}
	backupPath := backup.Name()
	if err := backup.Close(); err != nil {
		_ = os.Remove(backupPath)
		return fmt.Errorf("close replacement backup reservation: %w", err)
	}
	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("release replacement backup reservation: %w", err)
	}

	targetPtr, err := windows.UTF16PtrFromString(targetPath)
	if err != nil {
		return fmt.Errorf("encode replace target: %w", err)
	}
	stagedPtr, err := windows.UTF16PtrFromString(stagedPath)
	if err != nil {
		return fmt.Errorf("encode staged replacement: %w", err)
	}
	backupPtr, err := windows.UTF16PtrFromString(backupPath)
	if err != nil {
		return fmt.Errorf("encode replacement backup: %w", err)
	}

	r1, _, callErr := replaceFileW.Call(
		uintptr(unsafe.Pointer(targetPtr)),
		uintptr(unsafe.Pointer(stagedPtr)),
		uintptr(unsafe.Pointer(backupPtr)),
		0,
		0,
		0,
	)
	if r1 == 0 {
		if callErr == windows.ERROR_SUCCESS {
			callErr = syscall.EINVAL
		}
		replaceErr := os.NewSyscallError("ReplaceFileW", callErr)
		if _, backupErr := os.Lstat(backupPath); backupErr == nil {
			if restoreErr := os.Rename(backupPath, targetPath); restoreErr != nil {
				return errors.Join(replaceErr, fmt.Errorf("restore original runtime config from %s: %w", backupPath, restoreErr))
			}
		} else if !errors.Is(backupErr, fs.ErrNotExist) {
			return errors.Join(replaceErr, fmt.Errorf("inspect replacement backup %s: %w", backupPath, backupErr))
		}
		return replaceErr
	}

	if err := os.Remove(backupPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove committed replacement backup %s: %w", backupPath, err)
	}
	return nil
}
