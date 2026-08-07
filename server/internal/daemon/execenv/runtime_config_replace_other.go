//go:build !windows && !darwin

package execenv

import "os"

func replaceRuntimeConfigFile(stagedPath, targetPath string) error {
	return os.Rename(stagedPath, targetPath)
}
