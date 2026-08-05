//go:build !windows

package execenv

import "os"

func replaceRuntimeConfigFile(stagedPath, targetPath string) error {
	return os.Rename(stagedPath, targetPath)
}
