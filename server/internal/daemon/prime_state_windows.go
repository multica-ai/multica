//go:build windows

package daemon

import (
	"errors"
	"os"
	"path/filepath"
)

func securePrimeStateDir(profileDir string, components ...string) (string, error) {
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return "", err
	}
	path := profileDir
	for _, component := range components {
		path = filepath.Join(path, component)
		if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", err
		}
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", errors.New("Prime state component must be a real private directory")
		}
	}
	return path, nil
}
