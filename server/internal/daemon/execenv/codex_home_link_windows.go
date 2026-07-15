//go:build windows

package execenv

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

const (
	codexPluginCacheMaxFiles     = 25_000
	codexPluginCacheMaxFileBytes = 16 << 20
	codexPluginCacheMaxBytes     = 512 << 20
)

var (
	errUnsafeCodexPluginCacheEntry = errors.New("unsafe Codex plugin cache entry")
	errCodexPluginCacheLimit       = errors.New("Codex plugin cache snapshot limit exceeded")
)

// createDirLink creates a bounded task-local snapshot on Windows. A directory
// link or junction would expose owner state through the writable task tree.
func createDirLink(src, dst string) error {
	if err := rejectWindowsReparsePoint(src); err != nil {
		return err
	}
	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("inspect Codex plugin cache source: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: source is not a directory", errUnsafeCodexPluginCacheEntry)
	}

	parent := filepath.Dir(dst)
	staging, err := os.MkdirTemp(parent, ".codex-plugin-cache-snapshot-")
	if err != nil {
		return fmt.Errorf("create Codex plugin cache staging directory: %w", err)
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(staging)
		}
	}()

	limits := codexPluginCacheSnapshotLimits{}
	if err := copyCodexPluginCacheTree(src, staging, &limits); err != nil {
		return err
	}
	if err := os.Rename(staging, dst); err != nil {
		return fmt.Errorf("publish Codex plugin cache snapshot: %w", err)
	}
	published = true
	return nil
}

type codexPluginCacheSnapshotLimits struct {
	files int
	bytes int64
}

func copyCodexPluginCacheTree(src, dst string, limits *codexPluginCacheSnapshotLimits) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := rejectWindowsReparsePoint(path); err != nil {
			return err
		}

		relative, err := filepath.Rel(src, path)
		if err != nil {
			return fmt.Errorf("resolve Codex plugin cache entry: %w", err)
		}
		target := filepath.Join(dst, relative)
		if entry.IsDir() {
			if relative == "." {
				return nil
			}
			if err := os.Mkdir(target, 0o755); err != nil {
				return fmt.Errorf("create Codex plugin cache snapshot directory: %w", err)
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("%w: %s", errUnsafeCodexPluginCacheEntry, relative)
		}

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect Codex plugin cache file: %w", err)
		}
		limits.files++
		if limits.files > codexPluginCacheMaxFiles || info.Size() > codexPluginCacheMaxFileBytes ||
			limits.bytes > codexPluginCacheMaxBytes-info.Size() {
			return fmt.Errorf("%w: %s", errCodexPluginCacheLimit, relative)
		}
		limits.bytes += info.Size()
		if err := copyCodexPluginCacheFile(path, target, info.Size()); err != nil {
			return err
		}
		return nil
	})
}

func copyCodexPluginCacheFile(src, dst string, expectedSize int64) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open Codex plugin cache file: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create Codex plugin cache snapshot file: %w", err)
	}
	ok := false
	defer func() {
		_ = out.Close()
		if !ok {
			_ = os.Remove(dst)
		}
	}()

	written, err := io.CopyN(out, in, expectedSize+1)
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("copy Codex plugin cache file: %w", err)
	}
	if written != expectedSize {
		return fmt.Errorf("%w: source changed while copying", errUnsafeCodexPluginCacheEntry)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close Codex plugin cache snapshot file: %w", err)
	}
	ok = true
	return nil
}

func rejectWindowsReparsePoint(path string) error {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("encode Codex plugin cache path: %w", err)
	}
	attributes, err := windows.GetFileAttributes(pathPtr)
	if err != nil {
		return fmt.Errorf("inspect Codex plugin cache attributes: %w", err)
	}
	if attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("%w: reparse point %s", errUnsafeCodexPluginCacheEntry, path)
	}
	return nil
}

// createFileLink tries os.Symlink first. If that fails, it falls back to
// copying the file so the content is still available.
func createFileLink(src, dst string) error {
	if err := os.Symlink(src, dst); err == nil {
		return nil
	}
	return copyFile(src, dst)
}
