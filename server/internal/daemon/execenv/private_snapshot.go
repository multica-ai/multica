package execenv

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
)

const (
	codexAuthSnapshotMaxBytes  = 4 << 20
	cursorAuthSnapshotMaxBytes = 16 << 20
	openclawSnapshotMaxBytes   = 16 << 20
)

func snapshotRegularFile(source, target string, maxBytes int64) error {
	data, err := readStableRegularFile(source, maxBytes, nil)
	if err != nil {
		return err
	}
	return writePrivateSnapshot(target, data, maxBytes)
}

// readStableRegularFile rejects path indirection and verifies that the opened
// file retains one identity and metadata tuple for the entire bounded read.
// afterOpen is only used by deterministic race tests.
func readStableRegularFile(source string, maxBytes int64, afterOpen func()) ([]byte, error) {
	before, err := os.Lstat(source)
	if err != nil {
		return nil, fmt.Errorf("inspect snapshot source: %w", err)
	}
	if err := validateSnapshotSource(before, maxBytes); err != nil {
		return nil, err
	}

	in, err := os.Open(source)
	if err != nil {
		return nil, fmt.Errorf("open snapshot source: %w", err)
	}
	defer in.Close()

	opened, err := in.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect opened snapshot source: %w", err)
	}
	if !os.SameFile(before, opened) {
		return nil, fmt.Errorf("snapshot source identity changed before read")
	}
	if err := validateSnapshotSource(opened, maxBytes); err != nil {
		return nil, err
	}
	if afterOpen != nil {
		afterOpen()
	}

	data, err := io.ReadAll(io.LimitReader(in, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read snapshot source: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("snapshot source exceeds %d bytes", maxBytes)
	}

	after, err := in.Stat()
	if err != nil {
		return nil, fmt.Errorf("reinspect snapshot source: %w", err)
	}
	pathAfter, err := os.Lstat(source)
	if err != nil {
		return nil, fmt.Errorf("reinspect snapshot source path: %w", err)
	}
	if !sameSnapshotIdentity(opened, after) || !sameSnapshotIdentity(opened, pathAfter) {
		return nil, fmt.Errorf("snapshot source identity changed during read")
	}
	if err := validateSnapshotSource(after, maxBytes); err != nil {
		return nil, err
	}
	if after.Size() != int64(len(data)) {
		return nil, fmt.Errorf("snapshot source size changed during read")
	}
	return data, nil
}

func validateSnapshotSource(info fs.FileInfo, maxBytes int64) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("snapshot source must not be a symlink")
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("snapshot source must be a regular file")
	}
	if info.Size() < 0 || info.Size() > maxBytes {
		return fmt.Errorf("snapshot source exceeds %d bytes", maxBytes)
	}
	links, ok := snapshotLinkCount(info)
	if !ok {
		return fmt.Errorf("snapshot source link count is unavailable")
	}
	if links != 1 {
		return fmt.Errorf("snapshot source must have exactly one hard link")
	}
	return nil
}

func sameSnapshotIdentity(a, b fs.FileInfo) bool {
	return os.SameFile(a, b) &&
		a.Mode() == b.Mode() &&
		a.Size() == b.Size() &&
		a.ModTime().Equal(b.ModTime())
}

// snapshotLinkCount uses the platform stat object's Nlink field without
// importing an OS-specific syscall package into this cross-platform file.
// Unknown platforms fail closed rather than silently accepting hard links.
func snapshotLinkCount(info fs.FileInfo) (uint64, bool) {
	v := reflect.ValueOf(info.Sys())
	if !v.IsValid() {
		return 0, false
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return 0, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0, false
	}
	field := v.FieldByName("Nlink")
	if !field.IsValid() {
		return 0, false
	}
	switch field.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return field.Uint(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value := field.Int()
		if value < 0 {
			return 0, false
		}
		return uint64(value), true
	default:
		return 0, false
	}
}

func writePrivateSnapshot(target string, data []byte, maxBytes int64) error {
	if int64(len(data)) > maxBytes {
		return fmt.Errorf("snapshot data exceeds %d bytes", maxBytes)
	}
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create snapshot directory: %w", err)
	}
	if info, err := os.Lstat(target); err == nil {
		if info.IsDir() {
			return fmt.Errorf("snapshot target is a directory")
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect snapshot target: %w", err)
	}

	tmp, err := os.CreateTemp(parent, ".snapshot-*")
	if err != nil {
		return fmt.Errorf("create private snapshot: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("secure private snapshot: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("write private snapshot: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync private snapshot: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close private snapshot: %w", err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("publish private snapshot: %w", err)
	}
	if err := os.Chmod(target, 0o600); err != nil {
		return fmt.Errorf("secure published snapshot: %w", err)
	}
	return nil
}
