package mapcollector

import (
	"fmt"
	"os"
)

func ReadAndDestroyKeyFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("stat HMAC key file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("HMAC key path must be a regular file, not a symlink or device")
	}
	if info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("HMAC key file permissions must not grant group or other access")
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read HMAC key file: %w", err)
	}
	if len(key) < 32 {
		clear(key)
		return nil, fmt.Errorf("HMAC key must contain at least 32 bytes")
	}
	if err := destroyFile(path, info.Size()); err != nil {
		clear(key)
		return nil, err
	}
	return key, nil
}

func destroyFile(path string, size int64) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open HMAC key file for destruction: %w", err)
	}
	zero := make([]byte, 4096)
	remaining := size
	for remaining > 0 {
		chunk := int64(len(zero))
		if remaining < chunk {
			chunk = remaining
		}
		if _, err := f.Write(zero[:chunk]); err != nil {
			_ = f.Close()
			return fmt.Errorf("overwrite HMAC key file: %w", err)
		}
		remaining -= chunk
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync destroyed HMAC key file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close destroyed HMAC key file: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove destroyed HMAC key file: %w", err)
	}
	return nil
}
