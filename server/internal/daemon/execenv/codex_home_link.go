//go:build !windows

package execenv

import "os"

func createDirLink(src, dst string) error {
	return os.Symlink(src, dst)
}

func createFileLink(src, dst string) error {
	return os.Symlink(src, dst)
}
