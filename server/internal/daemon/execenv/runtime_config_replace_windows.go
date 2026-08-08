package execenv

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var replaceFileW = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReplaceFileW")

func validateRuntimeConfigReplacementTarget(string, fs.FileInfo) error {
	return nil
}

// preserveRuntimeConfigFileMetadata leaves existing-file metadata to
// ReplaceFileW, which transfers the target's ACL and filesystem attributes to
// the staged inode. Missing files retain the deliberately private 0600 mode.
func preserveRuntimeConfigFileMetadata(staged *os.File, sourcePath string, mode fs.FileMode) error {
	if sourcePath == "" {
		return staged.Chmod(mode)
	}
	return nil
}

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

	extendedTargetPath, err := extendedLengthWindowsPath(targetPath)
	if err != nil {
		return fmt.Errorf("normalize replace target: %w", err)
	}
	extendedStagedPath, err := extendedLengthWindowsPath(stagedPath)
	if err != nil {
		return fmt.Errorf("normalize staged replacement: %w", err)
	}
	extendedBackupPath, err := extendedLengthWindowsPath(backupPath)
	if err != nil {
		return fmt.Errorf("normalize replacement backup: %w", err)
	}

	targetPtr, err := windows.UTF16PtrFromString(extendedTargetPath)
	if err != nil {
		return fmt.Errorf("encode replace target: %w", err)
	}
	stagedPtr, err := windows.UTF16PtrFromString(extendedStagedPath)
	if err != nil {
		return fmt.Errorf("encode staged replacement: %w", err)
	}
	backupPtr, err := windows.UTF16PtrFromString(extendedBackupPath)
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

// extendedLengthWindowsPath gives raw Win32 APIs the same long-path support
// that Go's os package applies internally. ReplaceFileW otherwise receives an
// ordinary absolute path and can fail once the existing target exceeds
// MAX_PATH in a process without a long-path-aware manifest.
func extendedLengthWindowsPath(path string) (string, error) {
	if strings.HasPrefix(path, `\\?\`) {
		return path, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if strings.HasPrefix(abs, `\\`) {
		return `\\?\UNC\` + strings.TrimPrefix(abs, `\\`), nil
	}
	return `\\?\` + abs, nil
}
